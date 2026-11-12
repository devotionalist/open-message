package loadmgr

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/devotionalist/open-message/internal/metadata"
)

// Manager is the M2 semi-automatic LoadManager: density alerts + suggested unload.
type Manager struct {
	Meta          *metadata.Store
	Log           *slog.Logger
	MaxTopicsHint int
	mu            sync.Mutex
	alerts        []Alert
}

type Alert struct {
	At      time.Time
	Broker  string
	Topics  int
	Message string
}

func (m *Manager) Observe(brokerID string, topicCount int) {
	if m.MaxTopicsHint == 0 {
		m.MaxTopicsHint = 100
	}
	if topicCount <= m.MaxTopicsHint {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	a := Alert{At: time.Now(), Broker: brokerID, Topics: topicCount, Message: "bundle density high; suggest unload/split"}
	m.alerts = append(m.alerts, a)
	if m.Log != nil {
		m.Log.Warn("load density", "broker", brokerID, "topics", topicCount)
	}
}

func (m *Manager) Alerts() []Alert {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Alert, len(m.alerts))
	copy(out, m.alerts)
	return out
}

// SuggestUnload returns namespaces that look overloaded (placeholder for Admin).
func (m *Manager) SuggestUnload(ctx context.Context) ([]string, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, a := range m.alerts {
		out = append(out, a.Broker)
	}
	return out, nil
}

// BundleSplitSuggestion is F3.6 semi-auto split advice (hex range mid).
type BundleSplitSuggestion struct {
	Namespace string
	BundleID  string
	Left      string
	Right     string
	Reason    string
}

// SuggestSplit proposes splitting a full-range bundle at midpoint.
func (m *Manager) SuggestSplit(ns, bundleID string) BundleSplitSuggestion {
	return BundleSplitSuggestion{
		Namespace: ns,
		BundleID:  bundleID,
		Left:      "00000000-7fffffff",
		Right:     "80000000-ffffffff",
		Reason:    "density/load threshold",
	}
}
