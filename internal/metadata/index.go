package metadata

import (
	"context"
	"errors"
)

func (s *Store) GetIndex(ctx context.Context, tenant, ns, topic string) (Record[LedgerIndexValue], error) {
	return getJSON[LedgerIndexValue](ctx, s.Raw, IndexKey(tenant, ns, topic))
}

// CasIndex replaces the index when expectVersion matches (0 = create).
func (s *Store) CasIndex(ctx context.Context, tenant, ns, topic string, expectVersion uint64, v LedgerIndexValue) (uint64, error) {
	return casJSON(ctx, s.Raw, IndexKey(tenant, ns, topic), expectVersion, v)
}

// AppendOpenLedger CAS-es openLedgerId onto the chain (roll step 3).
func (s *Store) AppendOpenLedger(ctx context.Context, tenant, ns, topic string, newID uint64) error {
	key := IndexKey(tenant, ns, topic)
	for attempt := 0; attempt < 16; attempt++ {
		rec, err := getJSON[LedgerIndexValue](ctx, s.Raw, key)
		expect := uint64(0)
		v := LedgerIndexValue{LedgerIDs: []uint64{newID}, OpenLedgerID: &newID, OpenStateHint: "OPEN"}
		if err == nil {
			expect = rec.Version
			v = rec.Value
			for _, id := range v.LedgerIDs {
				if id == newID {
					// already linked — success (idempotent)
					if v.OpenLedgerID != nil && *v.OpenLedgerID == newID {
						return nil
					}
					v.OpenLedgerID = &newID
					v.OpenStateHint = "OPEN"
					_, err = casJSON(ctx, s.Raw, key, expect, v)
					if errors.Is(err, ErrVersionConflict) {
						continue
					}
					return err
				}
			}
			v.LedgerIDs = append(append([]uint64{}, v.LedgerIDs...), newID)
			v.OpenLedgerID = &newID
			v.OpenStateHint = "OPEN"
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		_, err = casJSON(ctx, s.Raw, key, expect, v)
		if errors.Is(err, ErrVersionConflict) {
			continue
		}
		return err
	}
	return ErrVersionConflict
}

func (s *Store) ClearOpenLedger(ctx context.Context, tenant, ns, topic string, closedID uint64) error {
	key := IndexKey(tenant, ns, topic)
	rec, err := getJSON[LedgerIndexValue](ctx, s.Raw, key)
	if err != nil {
		return err
	}
	v := rec.Value
	if v.OpenLedgerID != nil && *v.OpenLedgerID == closedID {
		v.OpenLedgerID = nil
		v.OpenStateHint = "NONE"
	}
	_, err = casJSON(ctx, s.Raw, key, rec.Version, v)
	return err
}
