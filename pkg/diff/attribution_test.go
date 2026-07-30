package diff

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/swarmviz/swarmviz/pkg/hub"
)

func TestAttributionSingleClaim(t *testing.T) {
	h := hub.NewEventHub()
	engine := NewAttributionEngine(h)
	defer engine.Close()

	claimID := h.ClaimFile("agent-alpha", "pkg/core/disk.go")
	defer h.ReleaseClaim(claimID)

	res := engine.Attribute("pkg/core/disk.go")

	if res.Attribution != AttributionClaimed {
		t.Errorf("expected attribution 'claimed', got '%s'", res.Attribution)
	}
	if res.AgentID != "agent-alpha" {
		t.Errorf("expected agent_id 'agent-alpha', got '%s'", res.AgentID)
	}
}

func TestAttributionZeroClaimsExternal(t *testing.T) {
	h := hub.NewEventHub()
	engine := NewAttributionEngine(h)
	defer engine.Close()

	res := engine.Attribute("unclaimed/file.go")

	if res.Attribution != AttributionExternal {
		t.Errorf("expected attribution 'external', got '%s'", res.Attribution)
	}
	if res.AgentID != AgentIDExternal {
		t.Errorf("expected agent_id 'external', got '%s'", res.AgentID)
	}
}

func TestAttributionMultipleClaimsConflict(t *testing.T) {
	h := hub.NewEventHub()
	engine := NewAttributionEngine(h)
	defer engine.Close()

	var conflictDetected, conflictResolved int32

	h.Subscribe(func(ev hub.HubEvent) {
		if ev.Type == "CONFLICT_DETECTED" {
			atomic.AddInt32(&conflictDetected, 1)
		}
		if ev.Type == "CONFLICT_RESOLVED" {
			atomic.AddInt32(&conflictResolved, 1)
		}
	})

	c1 := h.ClaimFile("agent-1", "shared/config.json")
	res1 := engine.Attribute("shared/config.json")
	if res1.Attribution != AttributionClaimed || res1.AgentID != "agent-1" {
		t.Fatalf("expected single claim attributed to agent-1")
	}

	c2 := h.ClaimFile("agent-2", "shared/config.json")
	res2 := engine.Attribute("shared/config.json")

	if res2.Attribution != AttributionConflict {
		t.Errorf("expected attribution 'conflict', got '%s'", res2.Attribution)
	}
	if res2.AgentID != AgentIDMulti {
		t.Errorf("expected agent_id 'multi', got '%s'", res2.AgentID)
	}

	if atomic.LoadInt32(&conflictDetected) != 1 {
		t.Errorf("expected 1 CONFLICT_DETECTED event")
	}

	conflicts := engine.GetActiveConflicts()
	if len(conflicts["shared/config.json"]) != 2 {
		t.Errorf("expected 2 conflicting agents in active conflicts map")
	}

	h.ReleaseClaim(c1)
	time.Sleep(10 * time.Millisecond)

	if atomic.LoadInt32(&conflictResolved) != 1 {
		t.Errorf("expected 1 CONFLICT_RESOLVED event after claim release")
	}

	res3 := engine.Attribute("shared/config.json")
	if res3.Attribution != AttributionClaimed || res3.AgentID != "agent-2" {
		t.Errorf("expected attribution falling back to agent-2, got attribution=%s, agent=%s", res3.Attribution, res3.AgentID)
	}

	h.ReleaseClaim(c2)
}

func TestAttributionTTLAutoExpiry(t *testing.T) {
	ttl := 40 * time.Millisecond
	h := hub.NewEventHub(hub.WithClaimTTL(ttl))
	engine := NewAttributionEngine(h)
	defer engine.Close()

	var ttlReleaseReceived int32
	h.Subscribe(func(ev hub.HubEvent) {
		if ev.Type == "AGENT_CLAIM_RELEASED" {
			if data, ok := ev.Data.(map[string]interface{}); ok {
				if data["reason"] == "ttl_expired" {
					atomic.AddInt32(&ttlReleaseReceived, 1)
				}
			}
		}
	})

	_ = h.ClaimFile("agent-temp", "temp/work.go")
	resBefore := engine.Attribute("temp/work.go")
	if resBefore.Attribution != AttributionClaimed {
		t.Fatalf("expected claimed before TTL expiry")
	}

	time.Sleep(80 * time.Millisecond)

	resAfter := engine.Attribute("temp/work.go")
	if resAfter.Attribution != AttributionExternal {
		t.Errorf("expected attribution 'external' after TTL expiry, got '%s'", resAfter.Attribution)
	}

	if atomic.LoadInt32(&ttlReleaseReceived) != 1 {
		t.Errorf("expected 1 AGENT_CLAIM_RELEASED event with reason ttl_expired")
	}
}

func TestAttributionRaceSafety(t *testing.T) {
	h := hub.NewEventHub(hub.WithClaimTTL(20 * time.Millisecond))
	engine := NewAttributionEngine(h)
	defer engine.Close()

	var wg sync.WaitGroup
	workers := 16
	iterations := 100

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			agentID := "agent-concurrent"
			filePath := "concurrent/file.go"
			for j := 0; j < iterations; j++ {
				cid := h.ClaimFile(agentID, filePath)
				_ = engine.Attribute(filePath)
				_ = engine.GetActiveConflicts()
				if j%2 == 0 {
					h.ReleaseClaim(cid)
				}
			}
		}(i)
	}

	wg.Wait()
}
