package agent

import (
	"math/rand/v2"
	"sync"
	"time"
)

type State string

const (
	StateInitializing State = "Initializing"
	StateUnconfigured State = "Unconfigured"
	StateUnpaired     State = "Unpaired"
	StateEnrolling    State = "Enrolling"
	StateConnecting   State = "Connecting"
	StatePaired       State = "Paired"
	StateFailed       State = "Failed"

	BackoffMinimum = time.Second
	BackoffMaximum = time.Minute
)

type Snapshot struct {
	State     State     `json:"state"`
	Reason    string    `json:"reason,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Status struct {
	mu       sync.RWMutex
	snapshot Snapshot
}

func NewStatus() *Status {
	return &Status{snapshot: Snapshot{State: StateInitializing, UpdatedAt: time.Now().UTC()}}
}

func (s *Status) Set(state State, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot = Snapshot{State: state, Reason: reason, UpdatedAt: time.Now().UTC()}
}

func (s *Status) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshot
}

func (s *Status) Ready() bool {
	state := s.Snapshot().State
	return state != StateInitializing && state != StateFailed
}

type Backoff struct {
	attempt uint
	random  *rand.Rand
}

func NewBackoff(seed uint64) *Backoff {
	return &Backoff{random: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))}
}

func (b *Backoff) Reset() { b.attempt = 0 }

func (b *Backoff) Next() time.Duration {
	base := BackoffMinimum << min(b.attempt, 5)
	if base > BackoffMaximum {
		base = BackoffMaximum
	}
	b.attempt++
	jitterRange := max(base/5, time.Millisecond)
	jitter := time.Duration(b.random.Int64N(int64(jitterRange*2))) - jitterRange
	delay := base + jitter
	if delay < BackoffMinimum {
		return BackoffMinimum
	}
	if delay > BackoffMaximum {
		return BackoffMaximum
	}
	return delay
}
