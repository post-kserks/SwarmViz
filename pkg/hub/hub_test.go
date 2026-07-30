package hub

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAgentLifecycle(t *testing.T) {
	h := NewEventHub()

	var eventCount int32
	h.Subscribe(func(ev HubEvent) {
		atomic.AddInt32(&eventCount, 1)
	})

	h.AgentCreated("agent-1", Orchestrator, "", "Main Orchestrator")
	h.AgentStatusChanged("agent-1", StatusRunning)

	state := h.GetInitState()
	agent, exists := state.Agents["agent-1"]
	if !exists {
		t.Fatalf("expected agent-1 in state")
	}
	if agent.Status != StatusRunning {
		t.Errorf("expected status RUNNING, got %s", agent.Status)
	}

	h.AgentTerminated("agent-1", "completed")
	state = h.GetInitState()
	if state.Agents["agent-1"].Status != StatusDone {
		t.Errorf("expected status DONE after termination, got %s", state.Agents["agent-1"].Status)
	}

	if atomic.LoadInt32(&eventCount) < 3 {
		t.Errorf("expected at least 3 events, got %d", eventCount)
	}
}

func TestAgentGraphEdges(t *testing.T) {
	h := NewEventHub()
	h.AgentCreated("agent-1", Orchestrator, "", "Root")
	h.AgentCreated("agent-2", Worker, "agent-1", "Worker 1")

	h.AgentEdge("agent-1", "agent-2", KindTaskDelegation)

	state := h.GetInitState()
	if len(state.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(state.Edges))
	}
	if state.Edges[0].FromID != "agent-1" || state.Edges[0].ToID != "agent-2" || state.Edges[0].Kind != KindTaskDelegation {
		t.Errorf("unexpected edge details: %+v", state.Edges[0])
	}
}

func TestClaimAndManualRelease(t *testing.T) {
	h := NewEventHub(WithClaimTTL(1 * time.Second))

	claimID := h.ClaimFile("agent-1", "src/main.go")
	if claimID == "" {
		t.Fatalf("expected valid claimID")
	}

	claims := h.GetActiveClaimsForFile("src/main.go")
	if len(claims) != 1 || claims[0] != "agent-1" {
		t.Errorf("expected active claim for agent-1, got %v", claims)
	}

	h.ReleaseClaim(claimID)
	claims = h.GetActiveClaimsForFile("src/main.go")
	if len(claims) != 0 {
		t.Errorf("expected no active claims after release, got %v", claims)
	}
}

func TestClaimTTLAutoExpiry(t *testing.T) {
	ttl := 50 * time.Millisecond
	h := NewEventHub(WithClaimTTL(ttl))

	var releasedEventReceived int32
	var releaseReason string
	var mu sync.Mutex

	h.Subscribe(func(ev HubEvent) {
		if ev.Type == "AGENT_CLAIM_RELEASED" {
			atomic.AddInt32(&releasedEventReceived, 1)
			data := ev.Data.(map[string]interface{})
			mu.Lock()
			releaseReason = data["reason"].(string)
			mu.Unlock()
		}
	})

	_ = h.ClaimFile("agent-1", "src/main.go")

	claims := h.GetActiveClaimsForFile("src/main.go")
	if len(claims) != 1 {
		t.Fatalf("expected 1 active claim initially")
	}

	time.Sleep(100 * time.Millisecond)

	claims = h.GetActiveClaimsForFile("src/main.go")
	if len(claims) != 0 {
		t.Errorf("expected 0 active claims after TTL expiry, got %v", len(claims))
	}

	if atomic.LoadInt32(&releasedEventReceived) != 1 {
		t.Errorf("expected 1 released event, got %d", releasedEventReceived)
	}

	mu.Lock()
	reason := releaseReason
	mu.Unlock()
	if reason != "ttl_expired" {
		t.Errorf("expected reason 'ttl_expired', got '%s'", reason)
	}
}

func TestConflictDetectionAndResolution(t *testing.T) {
	h := NewEventHub()

	var conflictDetected, conflictResolved int32

	h.Subscribe(func(ev HubEvent) {
		if ev.Type == "CONFLICT_DETECTED" {
			atomic.AddInt32(&conflictDetected, 1)
		}
		if ev.Type == "CONFLICT_RESOLVED" {
			atomic.AddInt32(&conflictResolved, 1)
		}
	})

	c1 := h.ClaimFile("agent-1", "pkg/core/disk.go")
	if atomic.LoadInt32(&conflictDetected) != 0 {
		t.Errorf("no conflict should be detected for 1 claim")
	}

	c2 := h.ClaimFile("agent-2", "pkg/core/disk.go")
	if atomic.LoadInt32(&conflictDetected) != 1 {
		t.Errorf("expected conflict detected on 2nd claim")
	}

	state := h.GetInitState()
	if len(state.Conflicts["pkg/core/disk.go"]) != 2 {
		t.Errorf("expected 2 conflict agents in state snapshot")
	}

	h.ReleaseClaim(c1)
	if atomic.LoadInt32(&conflictResolved) != 1 {
		t.Errorf("expected conflict resolved after releasing 1 claim")
	}

	h.ReleaseClaim(c2)
}

func TestAgentLogBuffering(t *testing.T) {
	h := NewEventHub(WithMaxLogsPerAgent(5))

	for i := 1; i <= 10; i++ {
		h.AgentLog("agent-1", "info", "log message")
	}

	logs := h.GetAgentLogs("agent-1", 10)
	if len(logs) != 5 {
		t.Errorf("expected max 5 log entries, got %d", len(logs))
	}
}

func TestControlHandlerRegistration(t *testing.T) {
	h := NewEventHub()

	err := h.ExecuteControlAction("agent-1", ActionPause)
	if err != ErrControlNotImplemented {
		t.Errorf("expected ErrControlNotImplemented, got %v", err)
	}

	var actionReceived ControlAction
	h.RegisterControlHandler(func(agentID string, action ControlAction) error {
		actionReceived = action
		return nil
	})

	err = h.ExecuteControlAction("agent-1", ActionPause)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if actionReceived != ActionPause {
		t.Errorf("expected ActionPause, got %s", actionReceived)
	}
}

func TestConcurrentClaimsAndReleases(t *testing.T) {
	h := NewEventHub(WithClaimTTL(20 * time.Millisecond))

	var wg sync.WaitGroup
	workers := 20
	opsPerWorker := 50

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			agentID := "agent-worker"
			for j := 0; j < opsPerWorker; j++ {
				cid := h.ClaimFile(agentID, "shared/file.go")
				if j%2 == 0 {
					h.ReleaseClaim(cid)
				}
				h.AgentLog(agentID, "debug", "working...")
				h.GetActiveClaimsForFile("shared/file.go")
				h.GetInitState()
			}
		}(i)
	}

	wg.Wait()
}
