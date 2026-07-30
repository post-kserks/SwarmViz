package server

import (
	"sync"
	"time"
)

type WSEnvelope struct {
	Version   int         `json:"v"`
	Seq       uint64      `json:"seq"`
	Timestamp string      `json:"ts"`
	Type      string      `json:"type"`
	Data      interface{} `json:"data"`
}

type RingBuffer struct {
	mu        sync.RWMutex
	capacity  int
	buf       []WSEnvelope
	head      int
	count     int
	nextSeq   uint64
	oldestSeq uint64
}

func NewRingBuffer(capacity int) *RingBuffer {
	if capacity <= 0 {
		capacity = 5000
	}
	return &RingBuffer{
		capacity: capacity,
		buf:      make([]WSEnvelope, capacity),
		nextSeq:  1,
	}
}

func (r *RingBuffer) Push(eventType string, data interface{}, ts time.Time) WSEnvelope {
	r.mu.Lock()
	defer r.mu.Unlock()

	if ts.IsZero() {
		ts = time.Now().UTC()
	} else {
		ts = ts.UTC()
	}

	seq := r.nextSeq
	r.nextSeq++

	env := WSEnvelope{
		Version:   1,
		Seq:       seq,
		Timestamp: ts.Format(time.RFC3339Nano),
		Type:      eventType,
		Data:      data,
	}

	r.buf[r.head] = env
	r.head = (r.head + 1) % r.capacity

	if r.count < r.capacity {
		r.count++
		if r.count == 1 {
			r.oldestSeq = seq
		}
	} else {
		r.oldestSeq = seq - uint64(r.capacity) + 1
	}

	return env
}

func (r *RingBuffer) GetSince(sinceSeq uint64) ([]WSEnvelope, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if sinceSeq == 0 {
		return nil, false
	}
	if r.count == 0 {
		return nil, false
	}

	newestSeq := r.nextSeq - 1

	if sinceSeq > newestSeq {
		return nil, false
	}

	if sinceSeq == newestSeq {
		return []WSEnvelope{}, true
	}

	if sinceSeq < r.oldestSeq-1 {
		return nil, false
	}

	numEvents := int(newestSeq - sinceSeq)
	result := make([]WSEnvelope, 0, numEvents)

	startIdx := (r.head - r.count + r.capacity) % r.capacity
	for i := 0; i < r.count; i++ {
		idx := (startIdx + i) % r.capacity
		env := r.buf[idx]
		if env.Seq > sinceSeq {
			result = append(result, env)
		}
	}

	return result, true
}

func (r *RingBuffer) GetRecentEvents(n int) []WSEnvelope {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.count == 0 || n <= 0 {
		return []WSEnvelope{}
	}

	take := n
	if take > r.count {
		take = r.count
	}

	result := make([]WSEnvelope, 0, take)
	startIdx := (r.head - r.count + r.capacity) % r.capacity
	skip := r.count - take

	for i := skip; i < r.count; i++ {
		idx := (startIdx + i) % r.capacity
		result = append(result, r.buf[idx])
	}

	return result
}

func (r *RingBuffer) GetNewestSeq() uint64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.count == 0 {
		return 0
	}
	return r.nextSeq - 1
}

func (r *RingBuffer) GetOldestSeq() uint64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.oldestSeq
}
