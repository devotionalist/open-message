package quota

import (
	"sync"
	"time"
)

// Policy is namespace publish/backlog limits.
type Policy struct {
	PublishRateMsg  int64 // 0 = unlimited
	PublishRateByte int64
	BacklogBytes    int64
	MinBrokers      int
	MinBookies      int
	IsolationGroup  string
}

func DefaultPolicy() Policy {
	return Policy{MinBrokers: 1, MinBookies: 1}
}

// Limiter is a simple token-bucket publish rate limiter per namespace.
type Limiter struct {
	mu       sync.Mutex
	policies map[string]Policy
	tokens   map[string]float64
	last     map[string]time.Time
	backlog  map[string]int64
}

func NewLimiter() *Limiter {
	return &Limiter{
		policies: map[string]Policy{},
		tokens:   map[string]float64{},
		last:     map[string]time.Time{},
		backlog:  map[string]int64{},
	}
}

func (l *Limiter) SetPolicy(ns string, p Policy) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.policies[ns] = p
}

func (l *Limiter) Policy(ns string) Policy {
	l.mu.Lock()
	defer l.mu.Unlock()
	if p, ok := l.policies[ns]; ok {
		return p
	}
	return DefaultPolicy()
}

func (l *Limiter) AllowPublish(ns string, bytes int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	p, ok := l.policies[ns]
	if !ok || p.PublishRateMsg <= 0 {
		return true
	}
	now := time.Now()
	last := l.last[ns]
	tok := l.tokens[ns]
	if last.IsZero() {
		tok = float64(p.PublishRateMsg)
	} else {
		tok += now.Sub(last).Seconds() * float64(p.PublishRateMsg)
		if tok > float64(p.PublishRateMsg) {
			tok = float64(p.PublishRateMsg)
		}
	}
	l.last[ns] = now
	if tok < 1 {
		l.tokens[ns] = tok
		return false
	}
	if p.PublishRateByte > 0 && int64(bytes) > p.PublishRateByte {
		l.tokens[ns] = tok
		return false
	}
	l.tokens[ns] = tok - 1
	return true
}

func (l *Limiter) AddBacklog(ns string, delta int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.backlog[ns] += delta
}

func (l *Limiter) BacklogOK(ns string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	p := l.policies[ns]
	if p.BacklogBytes <= 0 {
		return true
	}
	return l.backlog[ns] < p.BacklogBytes
}

// AllowScaleIn rejects if brokersAfter < min.
func AllowScaleIn(policy Policy, brokersAfter int) bool {
	min := policy.MinBrokers
	if min <= 0 {
		min = 1
	}
	return brokersAfter >= min
}
