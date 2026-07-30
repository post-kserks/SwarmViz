package diff

import (
	"path/filepath"
	"sync"

	"github.com/swarmviz/swarmviz/pkg/hub"
)

type ClaimProvider interface {
	GetActiveClaimsForFile(filePath string) []string
	Subscribe(listener hub.EventListener) func()
}

type AttributionEngine struct {
	provider    ClaimProvider
	mu          sync.RWMutex
	conflicts   map[string][]string
	unsubscribe func()
}

func NewAttributionEngine(provider ClaimProvider) *AttributionEngine {
	engine := &AttributionEngine{
		provider:  provider,
		conflicts: make(map[string][]string),
	}

	if provider != nil {
		engine.unsubscribe = provider.Subscribe(engine.handleHubEvent)
	}

	return engine
}

func (a *AttributionEngine) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.unsubscribe != nil {
		a.unsubscribe()
		a.unsubscribe = nil
	}
}

func (a *AttributionEngine) handleHubEvent(ev hub.HubEvent) {
	switch ev.Type {
	case "CONFLICT_DETECTED":
		if data, ok := ev.Data.(map[string]interface{}); ok {
			file, _ := data["file"].(string)
			if file != "" {
				cleanPath := filepath.Clean(file)
				var agentIDs []string
				if ids, ok := data["agent_ids"].([]string); ok {
					agentIDs = ids
				} else if rawIDs, ok := data["agent_ids"].([]interface{}); ok {
					for _, raw := range rawIDs {
						if s, ok := raw.(string); ok {
							agentIDs = append(agentIDs, s)
						}
					}
				}
				a.mu.Lock()
				a.conflicts[cleanPath] = agentIDs
				a.mu.Unlock()
			}
		}
	case "CONFLICT_RESOLVED":
		if data, ok := ev.Data.(map[string]interface{}); ok {
			file, _ := data["file"].(string)
			if file != "" {
				cleanPath := filepath.Clean(file)
				a.mu.Lock()
				delete(a.conflicts, cleanPath)
				a.mu.Unlock()
			}
		}
	}
}

func (a *AttributionEngine) Attribute(filePath string) AttributionResult {
	cleanPath := filepath.Clean(filePath)

	var claims []string
	if a.provider != nil {
		claims = a.provider.GetActiveClaimsForFile(cleanPath)
	}

	switch len(claims) {
	case 0:
		return AttributionResult{
			Attribution: AttributionExternal,
			AgentID:     AgentIDExternal,
			File:        cleanPath,
		}
	case 1:
		return AttributionResult{
			Attribution: AttributionClaimed,
			AgentID:     claims[0],
			File:        cleanPath,
			ClaimIDs:    claims,
		}
	default:
		a.mu.Lock()
		a.conflicts[cleanPath] = claims
		a.mu.Unlock()

		return AttributionResult{
			Attribution: AttributionConflict,
			AgentID:     AgentIDMulti,
			File:        cleanPath,
			ClaimIDs:    claims,
		}
	}
}

func (a *AttributionEngine) GetActiveConflicts() map[string][]string {
	a.mu.RLock()
	defer a.mu.RUnlock()

	result := make(map[string][]string, len(a.conflicts))
	for file, agents := range a.conflicts {
		cp := make([]string, len(agents))
		copy(cp, agents)
		result[file] = cp
	}
	return result
}
