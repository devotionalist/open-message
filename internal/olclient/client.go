package olclient

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"

	ledgerclient "github.com/devotionalist/open-ledger-client-go"
)

// ErrFenced / ErrUnavailable map OL errors to broker-visible classes.
var (
	ErrFenced      = errors.New("olclient: fenced")
	ErrUnavailable = errors.New("olclient: unavailable")
	ErrConflict    = errors.New("olclient: conflict")
	ErrSealed      = errors.New("olclient: sealed")
)

// QuorumPolicy is the ensemble intent Broker passes (Client selects bookies).
type QuorumPolicy struct {
	E, Qw, Qa uint32
}

// Position is ledgerId+entryId.
type Position struct {
	LedgerID uint64
	EntryID  int64
}

// Client is the Broker-facing open-ledger facade (includes MarkOrphan).
type Client interface {
	Create(ctx context.Context, policy QuorumPolicy) (ledgerID uint64, err error)
	Append(ctx context.Context, ledgerID uint64, payload []byte) (entryID uint64, err error)
	Close(ctx context.Context, ledgerID uint64) (lastEntryID int64, err error)
	FenceAndRecover(ctx context.Context, ledgerID uint64) (lastEntryID int64, err error)
	Read(ctx context.Context, ledgerID uint64, entryID uint64) ([]byte, error)
	MarkOrphan(ctx context.Context, ledgerID uint64) error
	IsOrphan(ctx context.Context, ledgerID uint64) (bool, error)
	LastEntryID(ctx context.Context, ledgerID uint64) (int64, error)
	OffloadCLOSED(ctx context.Context, ledgerID uint64, location string) error
	OffloadLocation(ctx context.Context, ledgerID uint64) (string, bool)
	// AdvanceEnsemble shifts the next Create/ChangeEnsemble bookie window (stride usually E).
	AdvanceEnsemble(stride int)
}

type writerSlot struct {
	w *ledgerclient.Writer
}

// Adapter wraps open-ledger-client-go with id allocation and orphan marks.
type Adapter struct {
	metas     ledgerclient.LedgerMetaStore
	bookies   map[ledgerclient.BookieID]ledgerclient.Bookie
	ids       []ledgerclient.BookieID
	nextID    atomic.Uint64
	ensStart  atomic.Uint64 // rotating window into ids for ensembles
	randomIDs bool          // shared remote meta: avoid multi-broker collisions

	mu      sync.Mutex
	writers map[uint64]*writerSlot
	orphans map[uint64]struct{}
}

func NewAdapter(metas ledgerclient.LedgerMetaStore, bookies map[ledgerclient.BookieID]ledgerclient.Bookie, startID uint64) *Adapter {
	ids := make([]ledgerclient.BookieID, 0, len(bookies))
	for id := range bookies {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	a := &Adapter{
		metas:   metas,
		bookies: bookies,
		ids:     ids,
		writers: map[uint64]*writerSlot{},
		orphans: map[uint64]struct{}{},
	}
	if startID == 0 {
		startID = 1
	}
	a.nextID.Store(startID)
	return a
}

// NewMemoryQa1 builds an in-process E=Qw=Qa=1 client for unit tests.
func NewMemoryQa1() *Adapter {
	b := ledgerclient.NewMemoryBookie()
	metas := ledgerclient.NewMemoryMetaStore()
	bookies := map[ledgerclient.BookieID]ledgerclient.Bookie{"b0": b}
	return NewAdapter(metas, bookies, 1)
}

func (a *Adapter) Create(_ context.Context, policy QuorumPolicy) (uint64, error) {
	if policy.E == 0 {
		policy = QuorumPolicy{E: 1, Qw: 1, Qa: 1}
	}
	q := ledgerclient.Quorum{E: policy.E, Qw: policy.Qw, Qa: policy.Qa}
	if err := q.Validate(); err != nil {
		return 0, err
	}
	if int(policy.E) > len(a.ids) {
		return 0, fmt.Errorf("%w: need %d bookies have %d", ErrUnavailable, policy.E, len(a.ids))
	}
	ensemble := a.pickEnsemble(int(policy.E))
	key := make([]byte, 16)
	_, _ = rand.Read(key)
	var ledgerID uint64
	var w *ledgerclient.Writer
	for attempt := 0; attempt < 8; attempt++ {
		ledgerID = a.allocLedgerID()
		var err error
		w, err = ledgerclient.Create(a.metas, a.bookies, ledgerID, q, ensemble, key)
		if err == nil {
			a.mu.Lock()
			a.writers[ledgerID] = &writerSlot{w: w}
			a.mu.Unlock()
			return ledgerID, nil
		}
		if ledgerclient.KindOf(err) != ledgerclient.KindAlreadyExists {
			return 0, mapErr(err)
		}
	}
	return 0, fmt.Errorf("%w: ledger id allocation exhausted", ErrUnavailable)
}

func (a *Adapter) pickEnsemble(e int) []ledgerclient.BookieID {
	n := len(a.ids)
	if e > n {
		e = n
	}
	start := int(a.ensStart.Load() % uint64(n))
	out := make([]ledgerclient.BookieID, e)
	for i := 0; i < e; i++ {
		out[i] = a.ids[(start+i)%n]
	}
	return out
}

// AdvanceEnsemble shifts the bookie window used by the next Create / ChangeEnsemble.
func (a *Adapter) AdvanceEnsemble(stride int) {
	if stride < 1 {
		stride = 1
	}
	a.ensStart.Add(uint64(stride))
}

// EnsembleOf returns the current write-set bookies for an open writer (tests / diagnostics).
func (a *Adapter) EnsembleOf(ledgerID uint64) ([]ledgerclient.BookieID, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	slot, ok := a.writers[ledgerID]
	if !ok {
		return nil, false
	}
	meta := slot.w.Meta()
	if len(meta.Fragments) == 0 {
		return nil, false
	}
	ens := meta.Fragments[len(meta.Fragments)-1].Ensemble
	return append([]ledgerclient.BookieID(nil), ens...), true
}

func (a *Adapter) allocLedgerID() uint64 {
	if !a.randomIDs {
		return a.nextID.Add(1) - 1
	}
	n := a.nextID.Add(1)
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return n
	}
	hi := binary.BigEndian.Uint64(b[:])
	if hi == 0 {
		hi = 1
	}
	return (hi << 16) ^ n
}

func (a *Adapter) Append(_ context.Context, ledgerID uint64, payload []byte) (uint64, error) {
	a.mu.Lock()
	slot, ok := a.writers[ledgerID]
	a.mu.Unlock()
	if !ok {
		return 0, ErrUnavailable
	}
	id, err := slot.w.Append(payload)
	if err == nil {
		return id, nil
	}
	// Not enough bookies in current fragment → switch ensemble in-place, then retry once.
	if ledgerclient.KindOf(err) == ledgerclient.KindNotEnoughBookies {
		if cerr := a.reensembleWriter(slot.w); cerr == nil {
			id, err = slot.w.Append(payload)
			if err == nil {
				return id, nil
			}
		}
	}
	return 0, mapErr(err)
}

func (a *Adapter) reensembleWriter(w *ledgerclient.Writer) error {
	meta := w.Meta()
	e := int(meta.Quorum.E)
	if e <= 0 {
		e = len(meta.Fragments[len(meta.Fragments)-1].Ensemble)
	}
	a.AdvanceEnsemble(e)
	ens := a.pickEnsemble(e)
	return w.ChangeEnsemble(w.NextEntryID(), ens)
}

func (a *Adapter) Close(_ context.Context, ledgerID uint64) (int64, error) {
	a.mu.Lock()
	slot, ok := a.writers[ledgerID]
	a.mu.Unlock()
	if !ok {
		// already closed / recovered path: close via meta LAC
		meta, err := a.metas.Get(ledgerID)
		if err != nil {
			return 0, mapErr(err)
		}
		last, err := ledgerclient.CloseLedger(a.metas, ledgerID, meta.LastEntryID)
		return last, mapErr(err)
	}
	last, err := slot.w.Close()
	a.mu.Lock()
	delete(a.writers, ledgerID)
	a.mu.Unlock()
	return last, mapErr(err)
}

func (a *Adapter) FenceAndRecover(_ context.Context, ledgerID uint64) (int64, error) {
	a.mu.Lock()
	delete(a.writers, ledgerID)
	a.mu.Unlock()
	last, err := ledgerclient.RecoverLedger(a.metas, a.bookies, ledgerID)
	return last, mapErr(err)
}

func (a *Adapter) Read(_ context.Context, ledgerID uint64, entryID uint64) ([]byte, error) {
	meta, err := a.metas.Get(ledgerID)
	if err != nil {
		return nil, mapErr(err)
	}
	e, err := ledgerclient.ReadEntry(a.bookies, meta, entryID)
	if err != nil {
		return nil, mapErr(err)
	}
	return e.Payload, nil
}

func (a *Adapter) MarkOrphan(_ context.Context, ledgerID uint64) error {
	a.mu.Lock()
	a.orphans[ledgerID] = struct{}{}
	a.mu.Unlock()
	// Best-effort: if meta supports extension via delete-after-mark, keep mark in-process.
	// Contract: Broker must call this after permanently abandoning Index link.
	return nil
}

func (a *Adapter) IsOrphan(_ context.Context, ledgerID uint64) (bool, error) {
	a.mu.Lock()
	_, ok := a.orphans[ledgerID]
	a.mu.Unlock()
	return ok, nil
}

func (a *Adapter) LastEntryID(_ context.Context, ledgerID uint64) (int64, error) {
	meta, err := a.metas.Get(ledgerID)
	if err != nil {
		return -1, mapErr(err)
	}
	return meta.LastEntryID, nil
}

func mapErr(err error) error {
	if err == nil {
		return nil
	}
	switch ledgerclient.KindOf(err) {
	case ledgerclient.KindFenced:
		return fmt.Errorf("%w: %v", ErrFenced, err)
	case ledgerclient.KindNotEnoughBookies, ledgerclient.KindVersionConflict, ledgerclient.KindMetaConflict:
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	case ledgerclient.KindConflict:
		return fmt.Errorf("%w: %v", ErrConflict, err)
	default:
		return err
	}
}

// MasterKeyHex helper for tests.
func MasterKeyHex(b []byte) string { return hex.EncodeToString(b) }
