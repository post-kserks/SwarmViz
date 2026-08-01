package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/swarmviz/swarmviz/pkg/hub"
)

func newAPITestServer(t *testing.T, opts ...ServerOption) (*Server, *hub.EventHub) {
	t.Helper()
	eventHub := hub.NewEventHub()
	srv := NewServer(eventHub, 100, nil, nil, opts...)
	t.Cleanup(srv.Close)
	return srv, eventHub
}

func doAPI(t *testing.T, srv *Server, method, path string, body interface{}, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("failed marshalling request body: %v", err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}

	req := httptest.NewRequest(method, path, reader)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestAPIHealthNeedsNoToken(t *testing.T) {
	srv, _ := newAPITestServer(t, WithAPIToken("s3cret"))

	rec := doAPI(t, srv, http.MethodGet, "/api/health", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for health, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestAPIAgentLifecycle(t *testing.T) {
	srv, eventHub := newAPITestServer(t)

	rec := doAPI(t, srv, http.MethodPost, "/api/agents", map[string]interface{}{
		"agent_id":   "root",
		"agent_type": "orchestrator",
		"label":      "Root",
	}, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create agent: expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}

	rec = doAPI(t, srv, http.MethodPost, "/api/agents/root/status", map[string]interface{}{
		"status": "WAITING",
	}, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status: expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}

	agents, _, _, _ := eventHub.GetInitSnapshot()
	agent, ok := agents["root"]
	if !ok {
		t.Fatal("agent 'root' missing from hub snapshot")
	}
	if agent.Status != hub.StatusWaiting {
		t.Errorf("expected status WAITING, got %s", agent.Status)
	}
	if agent.Type != hub.Orchestrator {
		t.Errorf("expected type orchestrator, got %s", agent.Type)
	}

	rec = doAPI(t, srv, http.MethodPost, "/api/agents/root/log", map[string]interface{}{
		"level":   "info",
		"message": "started",
	}, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("log: expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	if logs := eventHub.GetAgentLogs("root", 10); len(logs) != 1 || logs[0].Message != "started" {
		t.Errorf("expected the log entry to reach the hub, got %+v", logs)
	}

	rec = doAPI(t, srv, http.MethodPost, "/api/agents/root/terminate", map[string]interface{}{
		"reason": "completed",
	}, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("terminate: expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}

	agents, _, _, _ = eventHub.GetInitSnapshot()
	if agents["root"].Status != hub.StatusDone {
		t.Errorf("expected status DONE after terminate, got %s", agents["root"].Status)
	}
}

func TestAPIClaimAndRelease(t *testing.T) {
	srv, eventHub := newAPITestServer(t)

	doAPI(t, srv, http.MethodPost, "/api/agents", map[string]interface{}{
		"agent_id": "w1", "agent_type": "worker", "label": "Worker",
	}, nil)

	rec := doAPI(t, srv, http.MethodPost, "/api/claims", map[string]interface{}{
		"agent_id": "w1",
		"file":     "pkg/server/api.go",
	}, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("claim: expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}

	var claimResp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &claimResp); err != nil {
		t.Fatalf("failed decoding claim response: %v", err)
	}
	claimID := claimResp["claim_id"]
	if claimID == "" {
		t.Fatal("expected a non-empty claim_id")
	}

	if holders := eventHub.GetActiveClaimsForFile("pkg/server/api.go"); len(holders) != 1 {
		t.Fatalf("expected 1 claim holder, got %v", holders)
	}

	rec = doAPI(t, srv, http.MethodDelete, "/api/claims/"+claimID, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("release: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if holders := eventHub.GetActiveClaimsForFile("pkg/server/api.go"); len(holders) != 0 {
		t.Fatalf("expected the claim to be released, still held by %v", holders)
	}
}

func TestAPIRejectsInvalidPayloads(t *testing.T) {
	srv, _ := newAPITestServer(t)

	cases := []struct {
		name   string
		method string
		path   string
		body   interface{}
	}{
		{"unknown agent type", http.MethodPost, "/api/agents", map[string]interface{}{"agent_id": "a", "agent_type": "wizard"}},
		{"missing agent id", http.MethodPost, "/api/agents", map[string]interface{}{"agent_type": "worker"}},
		{"self parent", http.MethodPost, "/api/agents", map[string]interface{}{"agent_id": "a", "agent_type": "worker", "parent_id": "a"}},
		{"unknown status", http.MethodPost, "/api/agents/a/status", map[string]interface{}{"status": "SLEEPING"}},
		{"unknown log level", http.MethodPost, "/api/agents/a/log", map[string]interface{}{"level": "trace", "message": "hi"}},
		{"empty log message", http.MethodPost, "/api/agents/a/log", map[string]interface{}{"level": "info", "message": ""}},
		{"unknown edge kind", http.MethodPost, "/api/edges", map[string]interface{}{"from_id": "a", "to_id": "b", "kind": "GOSSIP"}},
		{"self edge", http.MethodPost, "/api/edges", map[string]interface{}{"from_id": "a", "to_id": "a", "kind": "DATA_PASS"}},
		{"claim without file", http.MethodPost, "/api/claims", map[string]interface{}{"agent_id": "a"}},
		{"unknown field", http.MethodPost, "/api/agents", map[string]interface{}{"agent_id": "a", "agent_type": "worker", "colour": "red"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doAPI(t, srv, tc.method, tc.path, tc.body, nil)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestAPIMethodAndRouteErrors(t *testing.T) {
	srv, _ := newAPITestServer(t)

	if rec := doAPI(t, srv, http.MethodGet, "/api/agents", nil, nil); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /api/agents: expected 405, got %d", rec.Code)
	}
	if rec := doAPI(t, srv, http.MethodPost, "/api/state", nil, nil); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /api/state: expected 405, got %d", rec.Code)
	}
	if rec := doAPI(t, srv, http.MethodGet, "/api/nope", nil, nil); rec.Code != http.StatusNotFound {
		t.Errorf("GET /api/nope: expected 404, got %d", rec.Code)
	}
	if rec := doAPI(t, srv, http.MethodPost, "/api/agents/a/dance", map[string]string{}, nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown agent action: expected 404, got %d", rec.Code)
	}
}

func TestAPITokenEnforcement(t *testing.T) {
	srv, _ := newAPITestServer(t, WithAPIToken("s3cret"))

	body := map[string]interface{}{"agent_id": "a", "agent_type": "worker"}

	if rec := doAPI(t, srv, http.MethodPost, "/api/agents", body, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: expected 401, got %d", rec.Code)
	}
	if rec := doAPI(t, srv, http.MethodPost, "/api/agents", body, map[string]string{
		"Authorization": "Bearer wrong",
	}); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong token: expected 401, got %d", rec.Code)
	}
	if rec := doAPI(t, srv, http.MethodPost, "/api/agents", body, map[string]string{
		"Authorization": "Bearer s3cret",
	}); rec.Code != http.StatusCreated {
		t.Errorf("valid token: expected 201, got %d", rec.Code)
	}
}

func TestAPIBatchAppliesInOrder(t *testing.T) {
	srv, eventHub := newAPITestServer(t)

	batch := []map[string]interface{}{
		{"type": "AGENT_CREATED", "data": map[string]interface{}{"agent_id": "root", "agent_type": "orchestrator", "label": "Root"}},
		{"type": "AGENT_CREATED", "data": map[string]interface{}{"agent_id": "w1", "agent_type": "worker", "parent_id": "root", "label": "Worker"}},
		{"type": "AGENT_EDGE", "data": map[string]interface{}{"from_id": "root", "to_id": "w1", "kind": "TASK_DELEGATION"}},
		{"type": "AGENT_EDIT_CLAIM", "data": map[string]interface{}{"agent_id": "w1", "file": "main.go"}},
		{"type": "AGENT_STATUS_CHANGED", "data": map[string]interface{}{"agent_id": "w1", "status": "RUNNING"}},
	}

	rec := doAPI(t, srv, http.MethodPost, "/api/events", batch, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("batch: expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}

	agents, edges, claims, _ := eventHub.GetInitSnapshot()
	if len(agents) != 2 {
		t.Errorf("expected 2 agents, got %d", len(agents))
	}
	if len(edges) != 1 || edges[0].Kind != hub.KindTaskDelegation {
		t.Errorf("expected one TASK_DELEGATION edge, got %+v", edges)
	}
	if len(claims) != 1 {
		t.Errorf("expected 1 active claim, got %d", len(claims))
	}
	if agents["w1"].Status != hub.StatusRunning {
		t.Errorf("expected w1 RUNNING, got %s", agents["w1"].Status)
	}
}

// A malformed entry must reject the whole batch, so the UI never shows a
// half-applied burst.
func TestAPIBatchIsAllOrNothing(t *testing.T) {
	srv, eventHub := newAPITestServer(t)

	batch := []map[string]interface{}{
		{"type": "AGENT_CREATED", "data": map[string]interface{}{"agent_id": "good", "agent_type": "worker"}},
		{"type": "AGENT_CREATED", "data": map[string]interface{}{"agent_id": "bad", "agent_type": "wizard"}},
	}

	rec := doAPI(t, srv, http.MethodPost, "/api/events", batch, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}

	agents, _, _, _ := eventHub.GetInitSnapshot()
	if len(agents) != 0 {
		t.Errorf("expected no agents to be applied, got %d", len(agents))
	}
}

func TestAPIBatchRejectsUnknownTypeAndEmptyList(t *testing.T) {
	srv, _ := newAPITestServer(t)

	rec := doAPI(t, srv, http.MethodPost, "/api/events", []map[string]interface{}{
		{"type": "SOMETHING_ELSE", "data": map[string]interface{}{}},
	}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unknown type: expected 400, got %d", rec.Code)
	}

	rec = doAPI(t, srv, http.MethodPost, "/api/events", []map[string]interface{}{}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty batch: expected 400, got %d", rec.Code)
	}
}

func TestAPIStateReflectsIngestedEvents(t *testing.T) {
	srv, _ := newAPITestServer(t)

	doAPI(t, srv, http.MethodPost, "/api/agents", map[string]interface{}{
		"agent_id": "root", "agent_type": "orchestrator", "label": "Root",
	}, nil)

	rec := doAPI(t, srv, http.MethodGet, "/api/state", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("state: expected 200, got %d", rec.Code)
	}

	var state struct {
		Agents map[string]struct {
			ID    string `json:"agent_id"`
			Label string `json:"label"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil {
		t.Fatalf("failed decoding state: %v", err)
	}
	if state.Agents["root"].Label != "Root" {
		t.Errorf("expected agent 'root' in state, got %+v", state.Agents)
	}
}

// An agent created without an explicit label should fall back to its id rather
// than rendering as a blank node.
func TestAPIAgentLabelDefaultsToID(t *testing.T) {
	srv, eventHub := newAPITestServer(t)

	doAPI(t, srv, http.MethodPost, "/api/agents", map[string]interface{}{
		"agent_id": "solo", "agent_type": "worker",
	}, nil)

	agents, _, _, _ := eventHub.GetInitSnapshot()
	if agents["solo"].Label != "solo" {
		t.Errorf("expected label to default to the id, got %q", agents["solo"].Label)
	}
}

// The task shown on a node can arrive either with the agent or on its own, and
// it must survive the periodic re-assert that keeps the node alive across a
// restart of the visualiser.
func TestAPIAgentTask(t *testing.T) {
	srv, eventHub := newAPITestServer(t)

	doAPI(t, srv, http.MethodPost, "/api/agents", map[string]interface{}{
		"agent_id": "root", "agent_type": "orchestrator", "label": "Root",
		"task": "ship the task labels",
	}, nil)

	agents, _, _, _ := eventHub.GetInitSnapshot()
	if agents["root"].Task != "ship the task labels" {
		t.Fatalf("task from create was not stored, got %q", agents["root"].Task)
	}

	rec := doAPI(t, srv, http.MethodPost, "/api/agents/root/task", map[string]interface{}{
		"task": "review the diff",
	}, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 for task update, got %d (%s)", rec.Code, rec.Body.String())
	}
	agents, _, _, _ = eventHub.GetInitSnapshot()
	if agents["root"].Task != "review the diff" {
		t.Fatalf("task was not updated, got %q", agents["root"].Task)
	}

	// Re-assert without a task: the node is recreated, the task stays.
	doAPI(t, srv, http.MethodPost, "/api/agents", map[string]interface{}{
		"agent_id": "root", "agent_type": "orchestrator", "label": "Root",
	}, nil)
	agents, _, _, _ = eventHub.GetInitSnapshot()
	if agents["root"].Task != "review the diff" {
		t.Fatalf("re-assert wiped the task, got %q", agents["root"].Task)
	}

	// An explicit empty task clears it — an idle agent works on nothing.
	doAPI(t, srv, http.MethodPost, "/api/agents/root/task", map[string]interface{}{"task": ""}, nil)
	agents, _, _, _ = eventHub.GetInitSnapshot()
	if agents["root"].Task != "" {
		t.Fatalf("empty task should clear the field, got %q", agents["root"].Task)
	}
}

// A whole user prompt can arrive as the task: it is flattened to one line and
// bounded, because the node renders it inline and every snapshot carries it.
func TestAPIAgentTaskIsFlattenedAndBounded(t *testing.T) {
	srv, eventHub := newAPITestServer(t)

	doAPI(t, srv, http.MethodPost, "/api/agents", map[string]interface{}{
		"agent_id": "w1", "agent_type": "worker",
	}, nil)
	doAPI(t, srv, http.MethodPost, "/api/agents/w1/task", map[string]interface{}{
		"task": "fix\n  the\tbuild",
	}, nil)

	agents, _, _, _ := eventHub.GetInitSnapshot()
	if agents["w1"].Task != "fix the build" {
		t.Fatalf("expected whitespace to be collapsed, got %q", agents["w1"].Task)
	}

	long := strings.Repeat("я", maxTaskRunes+50)
	doAPI(t, srv, http.MethodPost, "/api/agents/w1/task", map[string]interface{}{"task": long}, nil)
	agents, _, _, _ = eventHub.GetInitSnapshot()
	if got := []rune(agents["w1"].Task); len(got) != maxTaskRunes+1 {
		t.Fatalf("expected %d runes plus an ellipsis, got %d", maxTaskRunes, len(got))
	}
	if !strings.HasSuffix(agents["w1"].Task, "…") {
		t.Errorf("truncated task should end with an ellipsis, got %q", agents["w1"].Task)
	}
}
