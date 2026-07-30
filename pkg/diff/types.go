package diff

type DiffStatus string

const (
	StatusInProgress DiffStatus = "IN_PROGRESS"
	StatusDeleted    DiffStatus = "DELETED"
	StatusBinary     DiffStatus = "BINARY"
	StatusSkipped    DiffStatus = "SKIPPED"
)

type AttributionType string

const (
	AttributionClaimed  AttributionType = "claimed"
	AttributionExternal AttributionType = "external"
	AttributionConflict AttributionType = "conflict"
)

const (
	AgentIDExternal = "external"
	AgentIDMulti    = "multi"
)

type AttributionResult struct {
	Attribution AttributionType `json:"attribution"`
	AgentID     string          `json:"agent_id"`
	File        string          `json:"file"`
	ClaimIDs    []string        `json:"claim_ids,omitempty"`
}

type DiffResult struct {
	AgentID     string          `json:"agent_id"`
	File        string          `json:"file"`
	Status      DiffStatus      `json:"status"`
	Attribution AttributionType `json:"attribution"`
	Binary      bool            `json:"binary,omitempty"`
	Skipped     bool            `json:"skipped,omitempty"`
	Reason      string          `json:"reason,omitempty"`
	Hunk        string          `json:"hunk,omitempty"`
	Added       int             `json:"added"`
	Removed     int             `json:"removed"`
	Truncated   bool            `json:"truncated,omitempty"`
	ClaimIDs    []string        `json:"claim_ids,omitempty"`
}
