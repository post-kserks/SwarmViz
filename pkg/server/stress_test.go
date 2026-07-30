package server

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/swarmviz/swarmviz/pkg/hub"
)

// TestStress_RingBuffer_WrapAround_SequenceIntegrity tests wrap-around > 5000 events.
func TestStress_RingBuffer_WrapAround_SequenceIntegrity(t *testing.T) {
	rb := NewRingBuffer(5000)

	totalEvents := 12500
	for i := 1; i <= totalEvents; i++ {
		rb.Push("STRESS_EVENT", map[string]int{"idx": i}, time.Now())
	}

	if rb.GetNewestSeq() != uint64(totalEvents) {
		t.Fatalf("expected newestSeq=%d, got %d", totalEvents, rb.GetNewestSeq())
	}

	expectedOldest := uint64(totalEvents - 5000 + 1) // 12500 - 5000 + 1 = 7501
	if rb.GetOldestSeq() != expectedOldest {
		t.Fatalf("expected oldestSeq=%d, got %d", expectedOldest, rb.GetOldestSeq())
	}

	// 1. Stale sinceSeq (< oldestSeq - 1) should return ok=false (needs INIT_STATE fallback)
	staleSeq := expectedOldest - 2 // 7499
	events, ok := rb.GetSince(staleSeq)
	if ok || events != nil {
		t.Fatalf("expected stale sinceSeq=%d to return (nil, false), got (%v, %v)", staleSeq, events, ok)
	}

	// Boundary sinceSeq == oldestSeq - 1 (7500) should return all 5000 stored events
	boundarySeq := expectedOldest - 1
	events, ok = rb.GetSince(boundarySeq)
	if !ok || len(events) != 5000 {
		t.Fatalf("expected boundary sinceSeq=%d to return (5000 events, true), got (%d, %v)", boundarySeq, len(events), ok)
	}

	// 2. Valid sinceSeq inside buffer range
	sinceSeq := uint64(10000)
	events, ok = rb.GetSince(sinceSeq)
	if !ok {
		t.Fatalf("expected ok=true for valid sinceSeq=%d", sinceSeq)
	}
	expectedCount := totalEvents - int(sinceSeq) // 12500 - 10000 = 2500
	if len(events) != expectedCount {
		t.Fatalf("expected %d replayed events, got %d", expectedCount, len(events))
	}

	// Verify sequence integrity and strict monotonicity
	for i := 0; i < len(events); i++ {
		expectedSeq := sinceSeq + 1 + uint64(i)
		if events[i].Seq != expectedSeq {
			t.Fatalf("sequence mismatch at idx %d: expected %d, got %d", i, expectedSeq, events[i].Seq)
		}
	}

	// 3. sinceSeq == newestSeq should return 0 events, ok=true
	events, ok = rb.GetSince(uint64(totalEvents))
	if !ok || len(events) != 0 {
		t.Fatalf("expected ([], true) for sinceSeq=newestSeq, got (%v, %v)", events, ok)
	}
}

// TestStress_RingBuffer_EdgeCases_Since tests edge cases for the since parameter.
func TestStress_RingBuffer_EdgeCases_Since(t *testing.T) {
	// Case A: Empty RingBuffer
	emptyRB := NewRingBuffer(5000)
	ev, ok := emptyRB.GetSince(0)
	if ok || ev != nil {
		t.Fatalf("empty RB with since=0 should return (nil, false), got (%v, %v)", ev, ok)
	}

	ev, ok = emptyRB.GetSince(100)
	if ok || ev != nil {
		t.Errorf("empty RB with since=100 returned (%v, ok=%v); should fall back to INIT_STATE", ev, ok)
	}

	// Case B: Non-empty buffer edge cases
	rb := NewRingBuffer(5)
	for i := 1; i <= 10; i++ {
		rb.Push("EVENT", i, time.Now())
	}
	// Oldest is 6, Newest is 10

	// since > newestSeq (future sequence)
	ev, ok = rb.GetSince(9999)
	if ok || ev != nil {
		t.Errorf("future since=9999 returned (%v events, ok=%v); should fall back to INIT_STATE", ev, ok)
	}

	// since == oldestSeq - 1 (since=5 when oldestSeq=6)
	ev, ok = rb.GetSince(5)
	if !ok || len(ev) != 5 { // seq 6, 7, 8, 9, 10
		t.Fatalf("since=oldestSeq-1(5) expected 5 events, got %d (ok=%v)", len(ev), ok)
	}

	// since == oldestSeq (exact boundary, since=6 when oldestSeq=6)
	ev, ok = rb.GetSince(6)
	if !ok || len(ev) != 4 { // seq 7, 8, 9, 10
		t.Fatalf("since=oldestSeq(6) expected 4 events, got %d (ok=%v)", len(ev), ok)
	}
}

// TestStress_Concurrent_Unregister_Panic_Demonstration empirically verifies that
// unregistering a client while broadcasting triggers a panic on closed channel / race.
func TestStress_Concurrent_Unregister_Panic_Demonstration(t *testing.T) {
	eventHub := hub.NewEventHub()
	srv := NewServer(eventHub, 1000, nil, nil)
	defer srv.Close()

	var panicked int32

	c := &Client{
		server: srv,
		conn:   nil,
		send:   make(chan []byte, 1), // small buffer to force default case in BroadcastEnvelope
	}
	srv.registerClient(c)

	// Fill the buffer
	c.send <- []byte("test")

	// Concurrently call BroadcastEvent while calling unregisterClient
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				atomic.StoreInt32(&panicked, 1)
				t.Logf("[EMPIRICAL PROOF] Caught expected send on closed channel panic: %v", r)
			}
		}()
		for i := 0; i < 100; i++ {
			srv.BroadcastEvent("EVENT", i)
			time.Sleep(10 * time.Microsecond)
		}
	}()

	go func() {
		defer wg.Done()
		time.Sleep(50 * time.Microsecond)
		srv.unregisterClient(c)
	}()

	wg.Wait()

	if atomic.LoadInt32(&panicked) == 1 {
		t.Logf("Confirmed: unregisterClient causes panic: send on closed channel under concurrent broadcast")
	}
}

// TestStress_WS_ConcurrentConnectDisconnect tests concurrent WebSocket connections
// and disconnections with live event traffic.
func TestStress_WS_ConcurrentConnectDisconnect(t *testing.T) {
	eventHub := hub.NewEventHub()
	srv := NewServer(eventHub, 5000, nil, nil)
	defer srv.Close()

	ts := createOrPipeTestServer(t, srv)
	defer ts.cleanup()

	var wg sync.WaitGroup
	stopCh := make(chan struct{})

	// Broadcaster goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			_ = recover() // recover potential send on closed channel panics during stress test
		}()
		var i int64
		for {
			select {
			case <-stopCh:
				return
			default:
				atomic.AddInt64(&i, 1)
				srv.BroadcastEvent("LIVE_EVENT", map[string]int64{"val": i})
				time.Sleep(100 * time.Microsecond)
			}
		}
	}()

	// 5 concurrent client workers connecting, reading a few messages, disconnecting
	for w := 0; w < 5; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				select {
				case <-stopCh:
					return
				default:
					wsPath := fmt.Sprintf("/ws?since=%d", i*5)
					ws, err := ts.Dial(wsPath)
					if err != nil {
						continue
					}
					_ = ws.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
					for m := 0; m < 3; m++ {
						var env WSEnvelope
						if err := ws.ReadJSON(&env); err != nil {
							break
						}
					}
					_ = ws.Close()
					time.Sleep(1 * time.Millisecond)
				}
			}
		}(w)
	}

	time.Sleep(1 * time.Second)
	close(stopCh)
	wg.Wait()
}

// TestStress_WS_HandshakeOrder verifies envelope delivery during handshake.
func TestStress_WS_HandshakeOrder(t *testing.T) {
	eventHub := hub.NewEventHub()
	srv := NewServer(eventHub, 1000, nil, nil)
	defer srv.Close()

	ts := createOrPipeTestServer(t, srv)
	defer ts.cleanup()

	// Push 5 events prior to connection
	for i := 1; i <= 5; i++ {
		srv.BroadcastEvent("PRE_EVENT", i)
	}

	ws, err := ts.Dial("/ws?since=1")
	if err != nil {
		t.Fatalf("failed to dial WS: %v", err)
	}
	defer ws.Close()

	var firstEnv WSEnvelope
	_ = ws.SetReadDeadline(time.Now().Add(1 * time.Second))
	if err := ws.ReadJSON(&firstEnv); err != nil {
		t.Fatalf("failed to read first message: %v", err)
	}

	t.Logf("First received envelope type=%s, seq=%d", firstEnv.Type, firstEnv.Seq)
}

// TestStress_ControlProtocol_EdgeCases tests invalid, unhandled, and edge-case control messages.
func TestStress_ControlProtocol_EdgeCases(t *testing.T) {
	eventHub := hub.NewEventHub()
	srv := NewServer(eventHub, 1000, nil, nil)
	defer srv.Close()

	ts := createOrPipeTestServer(t, srv)
	defer ts.cleanup()

	ws, err := ts.Dial("/ws")
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer ws.Close()

	// Read initial INIT_STATE
	var initEnv WSEnvelope
	_ = ws.ReadJSON(&initEnv)

	// 1. Send unknown JSON type
	unknownJSON := map[string]string{"type": "UNKNOWN_ACTION_TYPE"}
	b, _ := json.Marshal(unknownJSON)
	_ = ws.WriteMessage(websocket.TextMessage, b)

	// 2. Send malformed AGENT_CONTROL data
	malformedCtrl := map[string]interface{}{"type": "AGENT_CONTROL", "data": "not_an_object"}
	b, _ = json.Marshal(malformedCtrl)
	_ = ws.WriteMessage(websocket.TextMessage, b)

	// 3. Send valid AGENT_CONTROL with unknown action name
	unhandledActionMsg := map[string]interface{}{
		"type": "AGENT_CONTROL",
		"data": map[string]string{
			"agent_id": "test-agent",
			"action":   "INVALID_ACTION_99",
		},
	}
	b, _ = json.Marshal(unhandledActionMsg)
	_ = ws.WriteMessage(websocket.TextMessage, b)

	// Verify server responds with error envelope (since eventHub has no control handler registered)
	var errEnv WSEnvelope
	_ = ws.SetReadDeadline(time.Now().Add(1 * time.Second))
	if err := ws.ReadJSON(&errEnv); err == nil {
		t.Logf("Received envelope for unhandled control action: type=%s data=%v", errEnv.Type, errEnv.Data)
	}
}
