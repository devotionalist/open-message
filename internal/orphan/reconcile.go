package orphan

import (
	"context"
	"log/slog"

	"github.com/devotionalist/open-message/internal/metadata"
	"github.com/devotionalist/open-message/internal/olclient"
)

// Reconciler does full Index↔OL reconciliation (no sampling deletes).
type Reconciler struct {
	Meta *metadata.Store
	OL   olclient.Client
	Log  *slog.Logger
}

type Report struct {
	Marked   int
	Indexed  int
	Orphans  []uint64
}

// ReconcileTopic marks ledgers created but never linked after permanent abandon.
// Linked ledgers are never deleted here.
func (r *Reconciler) ReconcileTopic(ctx context.Context, tenant, ns, topic string, candidates []uint64) (Report, error) {
	rep := Report{}
	rec, err := r.Meta.GetIndex(ctx, tenant, ns, topic)
	linked := map[uint64]struct{}{}
	if err == nil {
		for _, id := range rec.Value.LedgerIDs {
			linked[id] = struct{}{}
			rep.Indexed++
		}
	}
	for _, id := range candidates {
		if _, ok := linked[id]; ok {
			continue
		}
		if err := r.OL.MarkOrphan(ctx, id); err != nil {
			return rep, err
		}
		rep.Marked++
		rep.Orphans = append(rep.Orphans, id)
		if r.Log != nil {
			r.Log.Info("orphan marked", "ledger", id, "topic", topic)
		}
	}
	return rep, nil
}
