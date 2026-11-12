package metrics

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
)

// Counters are scrapeable via /metrics (JSON; Prometheus text optional later).
type Counters struct {
	ProduceOK      atomic.Uint64 `json:"-"`
	ProduceErr     atomic.Uint64 `json:"-"`
	Failover       atomic.Uint64 `json:"-"`
	NotOwner       atomic.Uint64 `json:"-"`
	OrphanMarked   atomic.Uint64 `json:"-"`
	OrphanDeleted  atomic.Uint64 `json:"-"`
	CursorLagMax   atomic.Int64  `json:"-"`
	Rolls          atomic.Uint64 `json:"-"`
	IdempotentHit  atomic.Uint64 `json:"-"`
}

func (c *Counters) Snapshot() map[string]uint64 {
	return map[string]uint64{
		"produce_ok":     c.ProduceOK.Load(),
		"produce_err":    c.ProduceErr.Load(),
		"failover":       c.Failover.Load(),
		"not_owner":      c.NotOwner.Load(),
		"orphan_marked":  c.OrphanMarked.Load(),
		"orphan_deleted": c.OrphanDeleted.Load(),
		"cursor_lag_max": uint64(c.CursorLagMax.Load()),
		"rolls":          c.Rolls.Load(),
		"idempotent_hit": c.IdempotentHit.Load(),
	}
}

func (c *Counters) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(c.Snapshot())
	}
}
