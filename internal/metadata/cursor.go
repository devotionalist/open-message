package metadata

import (
	"context"
	"errors"
)

func (s *Store) GetCursor(ctx context.Context, tenant, ns, topic, sub string) (Record[CursorValue], error) {
	return getJSON[CursorValue](ctx, s.Raw, CursorKey(tenant, ns, topic, sub))
}

// CasAdvance moves mark-delete forward only (no regress).
func (s *Store) CasAdvance(ctx context.Context, tenant, ns, topic, sub string, pos Position) error {
	key := CursorKey(tenant, ns, topic, sub)
	for attempt := 0; attempt < 16; attempt++ {
		rec, err := getJSON[CursorValue](ctx, s.Raw, key)
		expect := uint64(0)
		next := CursorValue{MarkDelete: pos}
		if err == nil {
			expect = rec.Version
			cur := rec.Value.MarkDelete
			if !positionAfter(pos, cur) {
				return nil // already at or past
			}
			next = rec.Value
			next.MarkDelete = pos
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		_, err = casJSON(ctx, s.Raw, key, expect, next)
		if errors.Is(err, ErrVersionConflict) {
			continue
		}
		return err
	}
	return ErrVersionConflict
}

func positionAfter(a, b Position) bool {
	if a.LedgerID != b.LedgerID {
		return a.LedgerID > b.LedgerID
	}
	return a.EntryID > b.EntryID
}

// CasAdvanceIndividual records an individual ack without jumping markDelete past holes.
func (s *Store) CasAdvanceIndividual(ctx context.Context, tenant, ns, topic, sub string, pos Position) error {
	key := CursorKey(tenant, ns, topic, sub)
	for attempt := 0; attempt < 16; attempt++ {
		rec, err := getJSON[CursorValue](ctx, s.Raw, key)
		expect := uint64(0)
		next := CursorValue{MarkDelete: Position{EntryID: -1}, IndividualAcks: []Position{pos}}
		if err == nil {
			expect = rec.Version
			next = rec.Value
			for _, p := range next.IndividualAcks {
				if p.LedgerID == pos.LedgerID && p.EntryID == pos.EntryID {
					return nil
				}
			}
			next.IndividualAcks = append(next.IndividualAcks, pos)
			// Contiguous prefix from markDelete can advance; holes block markDelete.
			next.MarkDelete = advancePastContiguous(next.MarkDelete, next.IndividualAcks)
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		_, err = casJSON(ctx, s.Raw, key, expect, next)
		if errors.Is(err, ErrVersionConflict) {
			continue
		}
		return err
	}
	return ErrVersionConflict
}

func advancePastContiguous(mark Position, acks []Position) Position {
	// Only advance mark when next entry is individually acked; never jump holes.
	next := mark
	if next.EntryID < 0 {
		next = Position{LedgerID: mark.LedgerID, EntryID: -1}
	}
	changed := true
	for changed {
		changed = false
		wantL, wantE := next.LedgerID, next.EntryID+1
		if next.EntryID < 0 {
			wantE = 0
		}
		for _, a := range acks {
			if a.LedgerID == wantL && a.EntryID == wantE {
				next = a
				changed = true
				break
			}
		}
	}
	return next
}
