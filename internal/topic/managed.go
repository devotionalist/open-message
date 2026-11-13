package topic

import (
	"context"
	"errors"
	"fmt"
	"sync"

	ledgerclient "github.com/devotionalist/open-ledger-client-go"
	"github.com/devotionalist/open-message/internal/metadata"
	"github.com/devotionalist/open-message/internal/olclient"
)

// ManagedLedger owns the write path for one topic (Owner-local).
type ManagedLedger struct {
	Tenant, Namespace, Topic string
	Meta                     *metadata.Store
	OL                       olclient.Client
	Policy                   olclient.QuorumPolicy

	mu       sync.Mutex
	ready    bool
	openID   uint64
	hasOpen  bool
	fenced   bool
}

func NewManagedLedger(tenant, ns, topic string, meta *metadata.Store, ol olclient.Client, policy olclient.QuorumPolicy) *ManagedLedger {
	if policy.E == 0 {
		policy = olclient.QuorumPolicy{E: 1, Qw: 1, Qa: 1}
	}
	return &ManagedLedger{
		Tenant: tenant, Namespace: ns, Topic: topic,
		Meta: meta, OL: ol, Policy: policy,
	}
}

func (m *ManagedLedger) Ready() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ready && !m.fenced
}

// Load recovers or creates an open ledger and becomes Ready.
func (m *ManagedLedger) Load(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, err := m.Meta.GetIndex(ctx, m.Tenant, m.Namespace, m.Topic)
	if err != nil && !errors.Is(err, metadata.ErrNotFound) {
		return err
	}
	if err == nil && rec.Value.OpenLedgerID != nil {
		openID := *rec.Value.OpenLedgerID
		if _, rerr := m.OL.FenceAndRecover(ctx, openID); rerr != nil {
			// Missing LedgerMeta (e.g. Index from in-process memory OL after switching to BOOKIE_ADDRS).
			if ledgerclient.KindOf(rerr) != ledgerclient.KindNotFound {
				return rerr
			}
			_ = m.OL.MarkOrphan(ctx, openID)
			// Drop ghost ledger chain so dispatcher won't read vanished ids.
			return m.resetIndexAndOpenLocked(ctx, rec.Version)
		}
		return m.openNewLocked(ctx)
	}
	// empty topic
	return m.openNewLocked(ctx)
}

func (m *ManagedLedger) openNewLocked(ctx context.Context) error {
	newID, err := m.OL.Create(ctx, m.Policy)
	if err != nil {
		return err
	}
	if err := m.Meta.AppendOpenLedger(ctx, m.Tenant, m.Namespace, m.Topic, newID); err != nil {
		_ = m.OL.MarkOrphan(ctx, newID)
		return err
	}
	m.openID = newID
	m.hasOpen = true
	m.ready = true
	return nil
}

func (m *ManagedLedger) resetIndexAndOpenLocked(ctx context.Context, expectVersion uint64) error {
	newID, err := m.OL.Create(ctx, m.Policy)
	if err != nil {
		return err
	}
	v := metadata.LedgerIndexValue{
		LedgerIDs:     []uint64{newID},
		OpenLedgerID:  &newID,
		OpenStateHint: "OPEN",
	}
	if _, err := m.Meta.CasIndex(ctx, m.Tenant, m.Namespace, m.Topic, expectVersion, v); err != nil {
		_ = m.OL.MarkOrphan(ctx, newID)
		return err
	}
	m.openID = newID
	m.hasOpen = true
	m.ready = true
	return nil
}

// Append writes one message; Receipt only after OL Qa.
// On Fenced / Sealed / Unavailable (e.g. ensemble bookies down), same-owner roll
// with ensemble rotation then retry once (design/03).
func (m *ManagedLedger) Append(ctx context.Context, payload []byte) (ledgerID uint64, entryID uint64, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.ready || m.fenced || !m.hasOpen {
		return 0, 0, fmt.Errorf("topic not ready")
	}
	eid, err := m.OL.Append(ctx, m.openID, payload)
	if err == nil {
		return m.openID, eid, nil
	}
	if !errors.Is(err, olclient.ErrFenced) && !errors.Is(err, olclient.ErrSealed) && !errors.Is(err, olclient.ErrUnavailable) {
		return 0, 0, err
	}
	// Prefer new bookie window before opening the replacement ledger.
	m.OL.AdvanceEnsemble(int(m.Policy.E))
	if rerr := m.rollLocked(ctx); rerr != nil {
		if errors.Is(err, olclient.ErrFenced) || errors.Is(err, olclient.ErrSealed) {
			m.fenced = true
			m.ready = false
		}
		return 0, 0, fmt.Errorf("%w (roll: %v)", err, rerr)
	}
	eid, err = m.OL.Append(ctx, m.openID, payload)
	if err != nil {
		return 0, 0, err
	}
	return m.openID, eid, nil
}

// Roll performs Close → Create → Index Cas; permanent Index failure → MarkOrphan.
func (m *ManagedLedger) Roll(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rollLocked(ctx)
}

func (m *ManagedLedger) rollLocked(ctx context.Context) error {
	if !m.hasOpen {
		return fmt.Errorf("no open ledger")
	}
	old := m.openID
	if _, err := m.OL.Close(ctx, old); err != nil {
		// Close is meta-only in OL client; if it fails, try Recover → CLOSED.
		if _, rerr := m.OL.FenceAndRecover(ctx, old); rerr != nil {
			return fmt.Errorf("seal old ledger: close=%v recover=%w", err, rerr)
		}
	}
	newID, err := m.OL.Create(ctx, m.Policy)
	if err != nil {
		m.ready = false
		return err
	}
	if err := m.Meta.AppendOpenLedger(ctx, m.Tenant, m.Namespace, m.Topic, newID); err != nil {
		_ = m.OL.MarkOrphan(ctx, newID)
		m.ready = false
		return err
	}
	m.openID = newID
	m.hasOpen = true
	m.ready = true
	m.fenced = false
	return nil
}

// ReadEntry reads a durable entry.
func (m *ManagedLedger) ReadEntry(ctx context.Context, ledgerID, entryID uint64) ([]byte, error) {
	return m.OL.Read(ctx, ledgerID, entryID)
}

// LastEntryID returns OL LAC for a ledger in the index chain.
func (m *ManagedLedger) LastEntryID(ctx context.Context, ledgerID uint64) (int64, error) {
	return m.OL.LastEntryID(ctx, ledgerID)
}

// FenceLocal stops writes (Unload / lost epoch).
func (m *ManagedLedger) FenceLocal() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fenced = true
	m.ready = false
}

// CloseForUnload gracefully seals the open ledger and clears Index open hint
// so the next Owner Load need not crash-Recover (design/09 §3.3).
func (m *ManagedLedger) CloseForUnload(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fenced = true
	m.ready = false
	if !m.hasOpen {
		return nil
	}
	old := m.openID
	m.hasOpen = false
	if _, err := m.OL.Close(ctx, old); err != nil {
		if _, rerr := m.OL.FenceAndRecover(ctx, old); rerr != nil {
			return fmt.Errorf("close for unload: close=%v recover=%w", err, rerr)
		}
	}
	if err := m.Meta.ClearOpenLedger(ctx, m.Tenant, m.Namespace, m.Topic, old); err != nil &&
		!errors.Is(err, metadata.ErrNotFound) && !errors.Is(err, metadata.ErrVersionConflict) {
		return err
	}
	return nil
}

func (m *ManagedLedger) OpenLedgerID() (uint64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.openID, m.hasOpen
}

// LedgerIDs returns the index chain.
func (m *ManagedLedger) LedgerIDs(ctx context.Context) ([]uint64, error) {
	rec, err := m.Meta.GetIndex(ctx, m.Tenant, m.Namespace, m.Topic)
	if err != nil {
		return nil, err
	}
	return append([]uint64{}, rec.Value.LedgerIDs...), nil
}
