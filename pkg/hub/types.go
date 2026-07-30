package hub

import (
	"errors"
	"time"
)

var (
	ErrControlNotImplemented = errors.New("Not implemented by orchestrator")
	ErrAgentNotFound         = errors.New("agent not found")
)

type AgentType string

const (
	Orchestrator AgentType = "orchestrator"
	Teamwork     AgentType = "teamwork"
	Challenger   AgentType = "challenger"
	Worker       AgentType = "worker"
)

type AgentStatus string

const (
	StatusIdle    AgentStatus = "IDLE"
	StatusRunning AgentStatus = "RUNNING"
	StatusWaiting AgentStatus = "WAITING"
	StatusDone    AgentStatus = "DONE"
	StatusError   AgentStatus = "ERROR"
)

type EdgeKind string

const (
	KindTaskDelegation EdgeKind = "TASK_DELEGATION"
	KindDataPass       EdgeKind = "DATA_PASS"
	KindReviewRequest  EdgeKind = "REVIEW_REQUEST"
)

type ControlAction string

const (
	ActionPause  ControlAction = "pause"
	ActionResume ControlAction = "resume"
)

type ReleaseReason string

const (
	ReasonReleased   ReleaseReason = "released"
	ReasonTTLExpired ReleaseReason = "ttl_expired"
)

type AgentNode struct {
	ID        string      `json:"agent_id"`
	Type      AgentType   `json:"agent_type"`
	ParentID  string      `json:"parent_id,omitempty"`
	Label     string      `json:"label"`
	Status    AgentStatus `json:"status"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

type AgentEdge struct {
	FromID    string    `json:"from_id"`
	ToID      string    `json:"to_id"`
	Kind      EdgeKind  `json:"kind"`
	CreatedAt time.Time `json:"created_at"`
}

type Claim struct {
	ID        string      `json:"claim_id"`
	AgentID   string      `json:"agent_id"`
	FilePath  string      `json:"file"`
	ClaimedAt time.Time   `json:"claimed_at"`
	ExpiresAt time.Time   `json:"expires_at"`
	timer     *time.Timer `json:"-"`
}

type ClaimExport struct {
	ClaimID   string    `json:"claim_id"`
	AgentID   string    `json:"agent_id"`
	FilePath  string    `json:"file"`
	ClaimedAt time.Time `json:"claimed_at"`
}

type ActiveClaimInfo = ClaimExport

type LogEntry struct {
	AgentID   string    `json:"agent_id"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

type HubEvent struct {
	Type      string      `json:"type"`
	Timestamp time.Time   `json:"ts"`
	Data      interface{} `json:"data"`
}

type EventListener func(event HubEvent)

type InitState struct {
	Agents       map[string]*AgentNode      `json:"agents"`
	Edges        []AgentEdge                `json:"edges"`
	ActiveClaims map[string]*ActiveClaimInfo `json:"active_claims"`
	Conflicts    map[string][]string        `json:"conflicts"`
}

type AgentEventHub interface {
	AgentCreated(id string, agentType AgentType, parentID string, label string)
	AgentStatusChanged(id string, status AgentStatus)
	AgentTerminated(id string, reason string)
	AgentEdge(fromID, toID string, kind EdgeKind)
	ClaimFile(agentID string, filePath string) (claimID string)
	ReleaseClaim(claimID string)
	AgentLog(agentID string, level string, message string)
	RegisterControlHandler(handler func(agentID string, action ControlAction) error)
	ExecuteControlAction(agentID string, action ControlAction) error
	GetActiveClaimsForFile(filePath string) []string
	Subscribe(listener EventListener) func()
	GetInitSnapshot() (agents map[string]*AgentNode, edges []AgentEdge, activeClaims map[string]*ActiveClaimInfo, conflicts map[string][]string)
}
