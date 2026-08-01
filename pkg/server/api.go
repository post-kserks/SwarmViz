package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/swarmviz/swarmviz/pkg/hub"
)

// The ingest API lets an orchestrator written in any language drive the
// visualiser over HTTP. Without it the AgentEventHub is reachable only from
// inside the Go process, which leaves the agent graph permanently empty when
// swarmviz runs as a standalone binary.

// maxAPIBodyBytes caps request bodies so a stray client cannot exhaust memory.
const maxAPIBodyBytes = 1 << 20 // 1MiB

// maxTaskRunes bounds the task text kept per agent. A task usually arrives as a
// whole user prompt, which can be arbitrarily long; the graph shows a couple of
// lines of it, so anything past this is dead weight in every snapshot and every
// WebSocket frame. Overlong text is trimmed rather than rejected — losing the
// tail of a prompt beats losing the task entirely.
const maxTaskRunes = 400

type agentCreateRequest struct {
	AgentID   string        `json:"agent_id"`
	AgentType hub.AgentType `json:"agent_type"`
	ParentID  string        `json:"parent_id"`
	Label     string        `json:"label"`
	Task      string        `json:"task"`
}

type agentTaskRequest struct {
	AgentID string `json:"agent_id"`
	Task    string `json:"task"`
}

type agentStatusRequest struct {
	AgentID string          `json:"agent_id"`
	Status  hub.AgentStatus `json:"status"`
}

type agentTerminateRequest struct {
	AgentID string `json:"agent_id"`
	Reason  string `json:"reason"`
}

type agentLogRequest struct {
	AgentID string `json:"agent_id"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

type edgeRequest struct {
	FromID string       `json:"from_id"`
	ToID   string       `json:"to_id"`
	Kind   hub.EdgeKind `json:"kind"`
}

type claimRequest struct {
	AgentID string `json:"agent_id"`
	File    string `json:"file"`
}

type batchEvent struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

var validAgentTypes = map[hub.AgentType]bool{
	hub.Orchestrator: true,
	hub.Teamwork:     true,
	hub.Challenger:   true,
	hub.Worker:       true,
}

var validStatuses = map[hub.AgentStatus]bool{
	hub.StatusIdle:    true,
	hub.StatusRunning: true,
	hub.StatusWaiting: true,
	hub.StatusDone:    true,
	hub.StatusError:   true,
}

var validEdgeKinds = map[hub.EdgeKind]bool{
	hub.KindTaskDelegation: true,
	hub.KindDataPass:       true,
	hub.KindReviewRequest:  true,
}

var validLogLevels = map[string]bool{
	"debug": true,
	"info":  true,
	"warn":  true,
	"error": true,
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeAPIError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeBody(w http.ResponseWriter, r *http.Request, dst interface{}) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAPIBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeAPIError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON body: %v", err))
		return false
	}
	return true
}

// authorized enforces the optional bearer token. The server binds to loopback
// by default, but --host can expose it, so a shared secret is available for
// that case.
func (s *Server) authorized(r *http.Request) bool {
	s.mu.RLock()
	token := s.apiToken
	s.mu.RUnlock()

	if token == "" {
		return true
	}
	header := r.Header.Get("Authorization")
	return strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")) == token
}

func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request) {
	route := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/"), "/")

	// Health stays unauthenticated so a supervisor can probe the port.
	if route == "health" {
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "GET required")
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"status": "ok"})
		return
	}

	if !s.authorized(r) {
		writeAPIError(w, http.StatusUnauthorized, "missing or invalid bearer token")
		return
	}

	if s.eventHub == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "event hub is not available")
		return
	}

	switch {
	case route == "state":
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "GET required")
			return
		}
		writeJSON(w, http.StatusOK, s.buildInitState())

	case route == "agents":
		s.handleAgentCreate(w, r)

	case route == "edges":
		s.handleEdge(w, r)

	case route == "claims":
		s.handleClaimCreate(w, r)

	case route == "events":
		s.handleBatch(w, r)

	case strings.HasPrefix(route, "claims/"):
		s.handleClaimRelease(w, r, strings.TrimPrefix(route, "claims/"))

	case strings.HasPrefix(route, "agents/"):
		s.handleAgentSubroute(w, r, strings.TrimPrefix(route, "agents/"))

	default:
		writeAPIError(w, http.StatusNotFound, "unknown endpoint")
	}
}

// handleAgentSubroute dispatches /api/agents/{id}/{action}.
func (s *Server) handleAgentSubroute(w http.ResponseWriter, r *http.Request, rest string) {
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 || parts[0] == "" {
		writeAPIError(w, http.StatusNotFound, "expected /api/agents/{id}/{status|task|terminate|log}")
		return
	}
	agentID, action := parts[0], parts[1]

	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	switch action {
	case "status":
		var req agentStatusRequest
		if !decodeBody(w, r, &req) {
			return
		}
		req.AgentID = agentID
		if err := s.applyAgentStatus(req); err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"agent_id": agentID})

	case "task":
		var req agentTaskRequest
		if !decodeBody(w, r, &req) {
			return
		}
		req.AgentID = agentID
		if err := s.applyAgentTask(req); err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"agent_id": agentID})

	case "terminate":
		var req agentTerminateRequest
		if !decodeBody(w, r, &req) {
			return
		}
		req.AgentID = agentID
		if err := s.applyAgentTerminate(req); err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"agent_id": agentID})

	case "log":
		var req agentLogRequest
		if !decodeBody(w, r, &req) {
			return
		}
		req.AgentID = agentID
		if err := s.applyAgentLog(req); err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"agent_id": agentID})

	default:
		writeAPIError(w, http.StatusNotFound, "unknown agent action")
	}
}

func (s *Server) handleAgentCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var req agentCreateRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if err := s.applyAgentCreate(req); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"agent_id": req.AgentID})
}

func (s *Server) handleEdge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var req edgeRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if err := s.applyEdge(req); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"from_id": req.FromID, "to_id": req.ToID})
}

func (s *Server) handleClaimCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var req claimRequest
	if !decodeBody(w, r, &req) {
		return
	}
	claimID, err := s.applyClaim(req)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"claim_id": claimID})
}

func (s *Server) handleClaimRelease(w http.ResponseWriter, r *http.Request, claimID string) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "DELETE or POST required")
		return
	}
	if claimID == "" {
		writeAPIError(w, http.StatusBadRequest, "claim_id is required")
		return
	}
	s.eventHub.ReleaseClaim(claimID)
	writeJSON(w, http.StatusOK, map[string]string{"claim_id": claimID})
}

// handleBatch applies a list of events in order, so an orchestrator can flush
// a burst of activity in one round trip. It is all-or-nothing on validation:
// the whole batch is checked before anything is applied, which keeps a typo in
// the last element from leaving half of it visible in the UI.
func (s *Server) handleBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	var events []batchEvent
	if !decodeBody(w, r, &events) {
		return
	}
	if len(events) == 0 {
		writeAPIError(w, http.StatusBadRequest, "batch must contain at least one event")
		return
	}

	decoded := make([]func() error, 0, len(events))
	for i, ev := range events {
		apply, err := s.prepareBatchEvent(ev)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, fmt.Sprintf("event %d: %v", i, err))
			return
		}
		decoded = append(decoded, apply)
	}

	for i, apply := range decoded {
		if err := apply(); err != nil {
			writeAPIError(w, http.StatusBadRequest, fmt.Sprintf("event %d: %v", i, err))
			return
		}
	}

	writeJSON(w, http.StatusAccepted, map[string]int{"applied": len(decoded)})
}

// prepareBatchEvent validates one batch entry and returns the deferred action.
func (s *Server) prepareBatchEvent(ev batchEvent) (func() error, error) {
	strict := func(dst interface{}) error {
		dec := json.NewDecoder(bytes.NewReader(ev.Data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(dst); err != nil {
			return fmt.Errorf("invalid data: %w", err)
		}
		return nil
	}

	switch ev.Type {
	case "AGENT_CREATED":
		var req agentCreateRequest
		if err := strict(&req); err != nil {
			return nil, err
		}
		if err := validateAgentCreate(req); err != nil {
			return nil, err
		}
		return func() error { return s.applyAgentCreate(req) }, nil

	case "AGENT_STATUS_CHANGED":
		var req agentStatusRequest
		if err := strict(&req); err != nil {
			return nil, err
		}
		if err := validateAgentStatus(req); err != nil {
			return nil, err
		}
		return func() error { return s.applyAgentStatus(req) }, nil

	case "AGENT_TASK_CHANGED":
		var req agentTaskRequest
		if err := strict(&req); err != nil {
			return nil, err
		}
		if err := validateAgentTask(req); err != nil {
			return nil, err
		}
		return func() error { return s.applyAgentTask(req) }, nil

	case "AGENT_TERMINATED":
		var req agentTerminateRequest
		if err := strict(&req); err != nil {
			return nil, err
		}
		if err := validateAgentTerminate(req); err != nil {
			return nil, err
		}
		return func() error { return s.applyAgentTerminate(req) }, nil

	case "AGENT_LOG":
		var req agentLogRequest
		if err := strict(&req); err != nil {
			return nil, err
		}
		if err := validateAgentLog(req); err != nil {
			return nil, err
		}
		return func() error { return s.applyAgentLog(req) }, nil

	case "AGENT_EDGE":
		var req edgeRequest
		if err := strict(&req); err != nil {
			return nil, err
		}
		if err := validateEdge(req); err != nil {
			return nil, err
		}
		return func() error { return s.applyEdge(req) }, nil

	case "AGENT_EDIT_CLAIM":
		var req claimRequest
		if err := strict(&req); err != nil {
			return nil, err
		}
		if err := validateClaim(req); err != nil {
			return nil, err
		}
		return func() error { _, err := s.applyClaim(req); return err }, nil

	case "AGENT_CLAIM_RELEASED":
		var req struct {
			ClaimID string `json:"claim_id"`
		}
		if err := strict(&req); err != nil {
			return nil, err
		}
		if req.ClaimID == "" {
			return nil, fmt.Errorf("claim_id is required")
		}
		return func() error { s.eventHub.ReleaseClaim(req.ClaimID); return nil }, nil

	default:
		return nil, fmt.Errorf("unsupported event type %q", ev.Type)
	}
}

func validateAgentCreate(req agentCreateRequest) error {
	if req.AgentID == "" {
		return fmt.Errorf("agent_id is required")
	}
	if !validAgentTypes[req.AgentType] {
		return fmt.Errorf("agent_type must be one of orchestrator, teamwork, challenger, worker")
	}
	if req.ParentID == req.AgentID {
		return fmt.Errorf("agent cannot be its own parent")
	}
	return nil
}

func validateAgentStatus(req agentStatusRequest) error {
	if req.AgentID == "" {
		return fmt.Errorf("agent_id is required")
	}
	if !validStatuses[req.Status] {
		return fmt.Errorf("status must be one of IDLE, RUNNING, WAITING, DONE, ERROR")
	}
	return nil
}

func validateAgentTask(req agentTaskRequest) error {
	if req.AgentID == "" {
		return fmt.Errorf("agent_id is required")
	}
	return nil
}

// trimTask collapses the task to a single line and bounds its length. The graph
// renders it inline, so an embedded newline would either be swallowed or blow
// the node's layout, and a multi-paragraph prompt is unreadable there anyway.
func trimTask(task string) string {
	task = strings.TrimSpace(strings.Join(strings.Fields(task), " "))
	runes := []rune(task)
	if len(runes) > maxTaskRunes {
		return strings.TrimSpace(string(runes[:maxTaskRunes])) + "…"
	}
	return task
}

func validateAgentTerminate(req agentTerminateRequest) error {
	if req.AgentID == "" {
		return fmt.Errorf("agent_id is required")
	}
	return nil
}

func validateAgentLog(req agentLogRequest) error {
	if req.AgentID == "" {
		return fmt.Errorf("agent_id is required")
	}
	if req.Message == "" {
		return fmt.Errorf("message is required")
	}
	if req.Level == "" {
		return fmt.Errorf("level is required")
	}
	if !validLogLevels[strings.ToLower(req.Level)] {
		return fmt.Errorf("level must be one of debug, info, warn, error")
	}
	return nil
}

func validateEdge(req edgeRequest) error {
	if req.FromID == "" || req.ToID == "" {
		return fmt.Errorf("from_id and to_id are required")
	}
	if req.FromID == req.ToID {
		return fmt.Errorf("from_id and to_id must differ")
	}
	if !validEdgeKinds[req.Kind] {
		return fmt.Errorf("kind must be one of TASK_DELEGATION, DATA_PASS, REVIEW_REQUEST")
	}
	return nil
}

func validateClaim(req claimRequest) error {
	if req.AgentID == "" {
		return fmt.Errorf("agent_id is required")
	}
	if req.File == "" {
		return fmt.Errorf("file is required")
	}
	return nil
}

func (s *Server) applyAgentCreate(req agentCreateRequest) error {
	if err := validateAgentCreate(req); err != nil {
		return err
	}
	label := req.Label
	if label == "" {
		label = req.AgentID
	}
	s.eventHub.AgentCreated(req.AgentID, req.AgentType, req.ParentID, label)
	// A creating client that already knows the task states it here, so one call
	// is enough on the common path (and a re-assert after a restart restores the
	// task along with the node). Omitting the field leaves whatever task the
	// agent already had untouched.
	if task := trimTask(req.Task); task != "" {
		s.eventHub.AgentTaskChanged(req.AgentID, task)
	}
	return nil
}

func (s *Server) applyAgentTask(req agentTaskRequest) error {
	if err := validateAgentTask(req); err != nil {
		return err
	}
	s.eventHub.AgentTaskChanged(req.AgentID, trimTask(req.Task))
	return nil
}

func (s *Server) applyAgentStatus(req agentStatusRequest) error {
	if err := validateAgentStatus(req); err != nil {
		return err
	}
	s.eventHub.AgentStatusChanged(req.AgentID, req.Status)
	return nil
}

func (s *Server) applyAgentTerminate(req agentTerminateRequest) error {
	if err := validateAgentTerminate(req); err != nil {
		return err
	}
	s.eventHub.AgentTerminated(req.AgentID, req.Reason)
	return nil
}

func (s *Server) applyAgentLog(req agentLogRequest) error {
	if err := validateAgentLog(req); err != nil {
		return err
	}
	s.eventHub.AgentLog(req.AgentID, strings.ToLower(req.Level), req.Message)
	return nil
}

func (s *Server) applyEdge(req edgeRequest) error {
	if err := validateEdge(req); err != nil {
		return err
	}
	s.eventHub.AgentEdge(req.FromID, req.ToID, req.Kind)
	return nil
}

func (s *Server) applyClaim(req claimRequest) (string, error) {
	if err := validateClaim(req); err != nil {
		return "", err
	}
	return s.eventHub.ClaimFile(req.AgentID, req.File), nil
}
