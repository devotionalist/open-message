package olclient

import (
	"context"
	"sync"
)

// OffloadStore tracks OffloadLocation on CLOSED ledgers (OL-side; Broker only initiates).
type OffloadStore struct {
	mu   sync.Mutex
	locs map[uint64]string
}

func (a *Adapter) ensureOffload() {
	// no-op; locations live on adapter
}

var offloadGlobal = struct {
	sync.Mutex
	m map[uint64]string
}{m: map[uint64]string{}}

// OffloadCLOSED records cold location for a sealed ledger (F3.7). Location never on OM Index.
func (a *Adapter) OffloadCLOSED(_ context.Context, ledgerID uint64, location string) error {
	last, err := a.LastEntryID(context.Background(), ledgerID)
	if err != nil {
		return err
	}
	_ = last
	offloadGlobal.Lock()
	offloadGlobal.m[ledgerID] = location
	offloadGlobal.Unlock()
	return nil
}

func (a *Adapter) OffloadLocation(_ context.Context, ledgerID uint64) (string, bool) {
	offloadGlobal.Lock()
	defer offloadGlobal.Unlock()
	s, ok := offloadGlobal.m[ledgerID]
	return s, ok
}
