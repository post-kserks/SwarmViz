package server

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestRingBuffer_PushAndGetSince(t *testing.T) {
	rb := NewRingBuffer(10)

	for i := 1; i <= 5; i++ {
		rb.Push("TEST_EVENT", map[string]int{"val": i}, time.Now())
	}

	if rb.GetNewestSeq() != 5 {
		t.Fatalf("expected newestSeq=5, got %d", rb.GetNewestSeq())
	}

	events, ok := rb.GetSince(0)
	if ok || events != nil {
		t.Fatalf("expected GetSince(0) to return (nil, false), got (%v, %v)", events, ok)
	}

	events, ok = rb.GetSince(2)
	if !ok || len(events) != 3 {
		t.Fatalf("expected 3 events for GetSince(2), got %d (ok=%v)", len(events), ok)
	}
	if events[0].Seq != 3 || events[2].Seq != 5 {
		t.Fatalf("expected seq range 3..5, got %d..%d", events[0].Seq, events[2].Seq)
	}

	events, ok = rb.GetSince(5)
	if !ok || len(events) != 0 {
		t.Fatalf("expected 0 events for GetSince(5), got %d (ok=%v)", len(events), ok)
	}
}

func TestRingBuffer_BufferOverflowFallback(t *testing.T) {
	rb := NewRingBuffer(5)

	for i := 1; i <= 10; i++ {
		rb.Push("TEST_EVENT", i, time.Now())
	}

	if rb.GetOldestSeq() != 6 {
		t.Fatalf("expected oldestSeq=6, got %d", rb.GetOldestSeq())
	}

	events, ok := rb.GetSince(3)
	if ok || events != nil {
		t.Fatalf("expected overflow fallback (nil, false), got (%v, %v)", events, ok)
	}

	events, ok = rb.GetSince(7)
	if !ok || len(events) != 3 {
		t.Fatalf("expected 3 events for GetSince(7), got %d", len(events))
	}
}

func TestRingBuffer_GetRecentEvents(t *testing.T) {
	rb := NewRingBuffer(10)

	for i := 1; i <= 5; i++ {
		rb.Push("TEST_EVENT", i, time.Now())
	}

	recent := rb.GetRecentEvents(3)
	if len(recent) != 3 {
		t.Fatalf("expected 3 recent events, got %d", len(recent))
	}
	if recent[0].Seq != 3 || recent[2].Seq != 5 {
		t.Fatalf("expected seq 3..5, got %d..%d", recent[0].Seq, recent[2].Seq)
	}
}

func TestRingBuffer_ConcurrentRaceSafety(t *testing.T) {
	rb := NewRingBuffer(100)
	var wg sync.WaitGroup

	for w := 0; w < 10; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				rb.Push("CONCURRENT_EVENT", fmt.Sprintf("%d-%d", writerID, i), time.Now())
			}
		}(w)
	}

	for r := 0; r < 5; r++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				_, _ = rb.GetSince(10)
				_ = rb.GetRecentEvents(20)
			}
		}(r)
	}

	wg.Wait()
}
