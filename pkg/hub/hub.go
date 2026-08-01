package hub

import (
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

type Option func(*EventHub)

func WithClaimTTL(d time.Duration) Option {
	return func(h *EventHub) {
		if d > 0 {
			h.claimTTL = d
		}
	}
}

func WithMaxLogsPerAgent(n int) Option {
	return func(h *EventHub) {
		if n > 0 {
			h.maxLogsPerAgent = n
		}
	}
}

type EventHub struct {
	mu              sync.RWMutex
	claimTTL        time.Duration
	maxLogsPerAgent int
	claimSeq        uint64
	nextListenerID  uint64

	agents         map[string]*AgentNode
	edges          []AgentEdge
	claimsByID     map[string]*Claim
	claimsByFile   map[string][]*Claim
	logs           map[string][]*LogEntry
	controlHandler func(agentID string, action ControlAction) error
	listeners      map[uint64]EventListener
}

var _ AgentEventHub = (*EventHub)(nil)

func NewEventHub(opts ...Option) *EventHub {
	h := &EventHub{
		claimTTL:        2 * time.Second,
		maxLogsPerAgent: 50,
		agents:          make(map[string]*AgentNode),
		edges:           make([]AgentEdge, 0),
		claimsByID:      make(map[string]*Claim),
		claimsByFile:    make(map[string][]*Claim),
		logs:            make(map[string][]*LogEntry),
		listeners:       make(map[uint64]EventListener),
	}

	for _, opt := range opts {
		opt(h)
	}
	return h
}

// AgentCreated adds a new agent to the hub.
func (h *EventHub) AgentCreated(id string, agentType AgentType, parentID string, label string) {
	if id == "" {
		return
	}
	now := time.Now().UTC()
	node := &AgentNode{
		ID:        id,
		Type:      agentType,
		ParentID:  parentID,
		Label:     label,
		Status:    StatusRunning,
		CreatedAt: now,
		UpdatedAt: now,
	}

	h.mu.Lock()
	// Creation is idempotent — clients re-assert their agent periodically so the
	// node survives a restart of the visualiser. A re-assert must not wipe the
	// task the agent is in the middle of; that is cleared or replaced only
	// through AgentTaskChanged.
	if prev, exists := h.agents[id]; exists {
		node.Task = prev.Task
		node.CreatedAt = prev.CreatedAt
	}
	h.agents[id] = node
	event := HubEvent{
		Type:      "AGENT_CREATED",
		Timestamp: now,
		Data: map[string]interface{}{
			"agent_id":   id,
			"agent_type": agentType,
			"parent_id":  parentID,
			"label":      label,
			"task":       node.Task,
		},
	}
	listeners := h.snapshotListenersLocked()
	h.mu.Unlock()

	h.dispatch(listeners, event)
}

// AgentTaskChanged records what the agent is working on right now. An empty
// task clears it — a session that finished its turn is not doing anything, and
// leaving the previous prompt on the node would misreport it as still busy.
//
// The event is emitted even for an unknown agent id: the client that reports
// tasks may race ahead of the one that creates the node, and dropping the
// update silently would strand the node without a task until the next change.
func (h *EventHub) AgentTaskChanged(id string, task string) {
	if id == "" {
		return
	}
	now := time.Now().UTC()

	h.mu.Lock()
	if agent, exists := h.agents[id]; exists {
		agent.Task = task
		agent.UpdatedAt = now
	}
	event := HubEvent{
		Type:      "AGENT_TASK_CHANGED",
		Timestamp: now,
		Data: map[string]interface{}{
			"agent_id": id,
			"task":     task,
		},
	}
	listeners := h.snapshotListenersLocked()
	h.mu.Unlock()

	h.dispatch(listeners, event)
}

// AgentStatusChanged updates an agent's status.
func (h *EventHub) AgentStatusChanged(id string, status AgentStatus) {
	now := time.Now().UTC()

	h.mu.Lock()
	agent, exists := h.agents[id]
	if exists {
		agent.Status = status
		agent.UpdatedAt = now
	}
	event := HubEvent{
		Type:      "AGENT_STATUS_CHANGED",
		Timestamp: now,
		Data: map[string]interface{}{
			"agent_id": id,
			"status":   status,
		},
	}
	listeners := h.snapshotListenersLocked()
	h.mu.Unlock()

	h.dispatch(listeners, event)
}

// AgentTerminated marks an agent as finished and releases active claims.
func (h *EventHub) AgentTerminated(id string, reason string) {
	now := time.Now().UTC()

	h.mu.Lock()
	agent, exists := h.agents[id]
	if exists {
		if reason == "error" || reason == "ERROR" {
			agent.Status = StatusError
		} else {
			agent.Status = StatusDone
		}
		agent.UpdatedAt = now
	}

	// Auto-release active claims held by this agent
	claimsToRelease := make([]string, 0)
	for claimID, c := range h.claimsByID {
		if c.AgentID == id {
			claimsToRelease = append(claimsToRelease, claimID)
		}
	}

	allEvents := make([]HubEvent, 0)
	for _, cid := range claimsToRelease {
		events, _ := h.releaseClaimLocked(cid, ReasonReleased)
		allEvents = append(allEvents, events...)
	}

	allEvents = append(allEvents, HubEvent{
		Type:      "AGENT_TERMINATED",
		Timestamp: now,
		Data: map[string]interface{}{
			"agent_id": id,
			"reason":   reason,
		},
	})

	listeners := h.snapshotListenersLocked()
	h.mu.Unlock()

	for _, ev := range allEvents {
		h.dispatch(listeners, ev)
	}
}

// AgentEdge registers a graph connection between two agents.
func (h *EventHub) AgentEdge(fromID, toID string, kind EdgeKind) {
	now := time.Now().UTC()
	edge := AgentEdge{
		FromID:    fromID,
		ToID:      toID,
		Kind:      kind,
		CreatedAt: now,
	}

	h.mu.Lock()
	h.edges = append(h.edges, edge)
	event := HubEvent{
		Type:      "AGENT_EDGE",
		Timestamp: now,
		Data: map[string]interface{}{
			"from_id": fromID,
			"to_id":   toID,
			"kind":    kind,
		},
	}
	listeners := h.snapshotListenersLocked()
	h.mu.Unlock()

	h.dispatch(listeners, event)
}

// ClaimFile claims a file for an agent with TTL auto-expiry.
func (h *EventHub) ClaimFile(agentID string, filePath string) string {
	cleanPath := filepath.Clean(filePath)
	seq := atomic.AddUint64(&h.claimSeq, 1)
	claimID := fmt.Sprintf("claim-%s-%d", agentID, seq)
	now := time.Now().UTC()
	expiresAt := now.Add(h.claimTTL)

	timer := time.AfterFunc(h.claimTTL, func() {
		h.expireClaim(claimID)
	})

	c := &Claim{
		ID:        claimID,
		AgentID:   agentID,
		FilePath:  cleanPath,
		ClaimedAt: now,
		ExpiresAt: expiresAt,
		timer:     timer,
	}

	h.mu.Lock()
	h.claimsByID[claimID] = c

	// Count distinct claimants before and after, so CONFLICT_DETECTED fires
	// once on the 1 -> 2 agents transition and never when an agent re-claims a
	// file it already holds.
	claimantsBefore := len(h.getClaimantIDsLocked(cleanPath))
	h.claimsByFile[cleanPath] = append(h.claimsByFile[cleanPath], c)
	claimantIDs := h.getClaimantIDsLocked(cleanPath)

	eventsToDispatch := make([]HubEvent, 0, 2)

	eventsToDispatch = append(eventsToDispatch, HubEvent{
		Type:      "AGENT_EDIT_CLAIM",
		Timestamp: now,
		Data: map[string]interface{}{
			"agent_id": agentID,
			"file":     cleanPath,
			"claim_id": claimID,
		},
	})

	if claimantsBefore == 1 && len(claimantIDs) == 2 {
		eventsToDispatch = append(eventsToDispatch, HubEvent{
			Type:      "CONFLICT_DETECTED",
			Timestamp: now,
			Data: map[string]interface{}{
				"file":      cleanPath,
				"agent_ids": claimantIDs,
			},
		})
	}

	listeners := h.snapshotListenersLocked()
	h.mu.Unlock()

	for _, ev := range eventsToDispatch {
		h.dispatch(listeners, ev)
	}

	return claimID
}

// ReleaseClaim releases an active claim by ID.
func (h *EventHub) ReleaseClaim(claimID string) {
	h.mu.Lock()
	events, listeners := h.releaseClaimLocked(claimID, ReasonReleased)
	h.mu.Unlock()

	for _, ev := range events {
		h.dispatch(listeners, ev)
	}
}

func (h *EventHub) expireClaim(claimID string) {
	h.mu.Lock()
	events, listeners := h.releaseClaimLocked(claimID, ReasonTTLExpired)
	h.mu.Unlock()

	for _, ev := range events {
		h.dispatch(listeners, ev)
	}
}

func (h *EventHub) releaseClaimLocked(claimID string, reason ReleaseReason) ([]HubEvent, []EventListener) {
	c, exists := h.claimsByID[claimID]
	if !exists {
		return nil, nil
	}

	if c.timer != nil {
		c.timer.Stop()
	}

	cleanPath := c.FilePath
	delete(h.claimsByID, claimID)

	fileClaims := h.claimsByFile[cleanPath]
	updated := make([]*Claim, 0, len(fileClaims))
	for _, fc := range fileClaims {
		if fc.ID != claimID {
			updated = append(updated, fc)
		}
	}

	// Distinct claimants, to mirror CONFLICT_DETECTED: dropping one of an
	// agent's own duplicate claims never resolves anything, and the file stays
	// conflicted until a whole agent lets go.
	prevCount := len(distinctAgentIDs(fileClaims))
	newCount := len(distinctAgentIDs(updated))

	if len(updated) == 0 {
		delete(h.claimsByFile, cleanPath)
	} else {
		h.claimsByFile[cleanPath] = updated
	}

	now := time.Now().UTC()
	events := make([]HubEvent, 0, 2)

	events = append(events, HubEvent{
		Type:      "AGENT_CLAIM_RELEASED",
		Timestamp: now,
		Data: map[string]interface{}{
			"claim_id": claimID,
			"file":     cleanPath,
			"reason":   string(reason),
		},
	})

	if prevCount >= 2 && newCount <= 1 {
		events = append(events, HubEvent{
			Type:      "CONFLICT_RESOLVED",
			Timestamp: now,
			Data: map[string]interface{}{
				"file": cleanPath,
			},
		})
	}

	return events, h.snapshotListenersLocked()
}

// AgentLog appends a log entry for an agent (ring buffered).
func (h *EventHub) AgentLog(agentID string, level string, message string) {
	now := time.Now().UTC()
	entry := &LogEntry{
		AgentID:   agentID,
		Level:     level,
		Message:   message,
		Timestamp: now,
	}

	h.mu.Lock()
	logs := h.logs[agentID]
	logs = append(logs, entry)
	if len(logs) > h.maxLogsPerAgent {
		logs = logs[len(logs)-h.maxLogsPerAgent:]
	}
	h.logs[agentID] = logs

	event := HubEvent{
		Type:      "AGENT_LOG",
		Timestamp: now,
		Data:      entry,
	}
	listeners := h.snapshotListenersLocked()
	h.mu.Unlock()

	h.dispatch(listeners, event)
}

// RegisterControlHandler registers a callback for UI control actions.
func (h *EventHub) RegisterControlHandler(handler func(agentID string, action ControlAction) error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.controlHandler = handler
}

// ExecuteControlAction executes a control action via the registered handler.
func (h *EventHub) ExecuteControlAction(agentID string, action ControlAction) error {
	h.mu.RLock()
	handler := h.controlHandler
	h.mu.RUnlock()

	if handler == nil {
		return ErrControlNotImplemented
	}
	return handler(agentID, action)
}

// Subscribe registers an EventListener. Returns an unsubscribe func.
func (h *EventHub) Subscribe(listener EventListener) func() {
	h.mu.Lock()
	id := h.nextListenerID
	h.nextListenerID++
	h.listeners[id] = listener
	h.mu.Unlock()

	return func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.listeners, id)
	}
}

// GetActiveClaimsForFile returns active claimant agentIDs for a file.
func (h *EventHub) GetActiveClaimsForFile(filePath string) []string {
	cleanPath := filepath.Clean(filePath)
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.getClaimantIDsLocked(cleanPath)
}

// GetAgentLogs returns the most recent log entries for an agent.
func (h *EventHub) GetAgentLogs(agentID string, maxCount int) []LogEntry {
	h.mu.RLock()
	defer h.mu.RUnlock()

	logs, exists := h.logs[agentID]
	if !exists || len(logs) == 0 {
		return []LogEntry{}
	}

	if maxCount <= 0 || maxCount > len(logs) {
		maxCount = len(logs)
	}

	startIdx := len(logs) - maxCount
	result := make([]LogEntry, maxCount)
	for i := 0; i < maxCount; i++ {
		result[i] = *logs[startIdx+i]
	}
	return result
}

// GetInitState constructs a full state snapshot for WebSocket initialization.
func (h *EventHub) GetInitState() InitState {
	h.mu.RLock()
	defer h.mu.RUnlock()

	agentsCopy := make(map[string]*AgentNode, len(h.agents))
	for k, v := range h.agents {
		n := *v
		agentsCopy[k] = &n
	}

	edgesCopy := make([]AgentEdge, len(h.edges))
	copy(edgesCopy, h.edges)

	claimsCopy := make(map[string]*ClaimExport, len(h.claimsByID))
	for _, c := range h.claimsByID {
		claimsCopy[c.FilePath] = &ClaimExport{
			ClaimID:   c.ID,
			AgentID:   c.AgentID,
			FilePath:  c.FilePath,
			ClaimedAt: c.ClaimedAt,
		}
	}

	conflictsCopy := make(map[string][]string)
	for file, claims := range h.claimsByFile {
		// Distinct agents, same rule as CONFLICT_DETECTED: a file is only
		// conflicted when two different agents hold it.
		if ids := distinctAgentIDs(claims); len(ids) >= 2 {
			conflictsCopy[file] = ids
		}
	}

	return InitState{
		Agents:       agentsCopy,
		Edges:        edgesCopy,
		ActiveClaims: claimsCopy,
		Conflicts:    conflictsCopy,
	}
}

// GetInitSnapshot implements AgentEventHub.
func (h *EventHub) GetInitSnapshot() (map[string]*AgentNode, []AgentEdge, map[string]*ActiveClaimInfo, map[string][]string) {
	st := h.GetInitState()
	return st.Agents, st.Edges, st.ActiveClaims, st.Conflicts
}

func (h *EventHub) getClaimantIDsLocked(cleanPath string) []string {
	return distinctAgentIDs(h.claimsByFile[cleanPath])
}

// distinctAgentIDs returns the claimants of a file, one entry per agent, in
// order of first claim.
//
// One agent can legitimately hold several live claims on the same path: it
// edits the file again before the previous claim's TTL runs out. That is not a
// conflict — a conflict is two DIFFERENT agents sharing a file. Counting raw
// claims here made an agent collide with itself, which pushed the edit to
// attribution `conflict` / agent `multi` and lost the real author.
func distinctAgentIDs(claims []*Claim) []string {
	if len(claims) == 0 {
		return []string{}
	}
	ids := make([]string, 0, len(claims))
	seen := make(map[string]struct{}, len(claims))
	for _, c := range claims {
		if _, dup := seen[c.AgentID]; dup {
			continue
		}
		seen[c.AgentID] = struct{}{}
		ids = append(ids, c.AgentID)
	}
	return ids
}

func (h *EventHub) snapshotListenersLocked() []EventListener {
	cp := make([]EventListener, 0, len(h.listeners))
	for _, l := range h.listeners {
		cp = append(cp, l)
	}
	return cp
}

func (h *EventHub) dispatch(listeners []EventListener, event HubEvent) {
	for _, l := range listeners {
		l(event)
	}
}
