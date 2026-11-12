package metadata

import (
	"context"
	"errors"
)

// IdemValue is M3 durable producer idempotency window (topic-scoped key).
type IdemValue struct {
	LastSeq  uint64            `json:"lastSeq"`
	Receipts map[string]MsgID  `json:"receipts"` // seq string -> message id
}

type MsgID struct {
	LedgerID uint64 `json:"ledgerId"`
	EntryID  uint64 `json:"entryId"`
}

func IdemKey(tenant, ns, topic, producer string) string {
	return "idem/" + tenant + "/" + ns + "/" + topic + "/" + producer
}

func (s *Store) GetIdem(ctx context.Context, tenant, ns, topic, producer string) (Record[IdemValue], error) {
	return getJSON[IdemValue](ctx, s.Raw, IdemKey(tenant, ns, topic, producer))
}

func (s *Store) PutIdemReceipt(ctx context.Context, tenant, ns, topic, producer string, seq, ledgerID, entryID uint64) error {
	key := IdemKey(tenant, ns, topic, producer)
	for i := 0; i < 16; i++ {
		rec, err := getJSON[IdemValue](ctx, s.Raw, key)
		expect := uint64(0)
		v := IdemValue{LastSeq: seq, Receipts: map[string]MsgID{}}
		if err == nil {
			expect = rec.Version
			v = rec.Value
			if v.Receipts == nil {
				v.Receipts = map[string]MsgID{}
			}
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		seqKey := itoa(seq)
		if existing, ok := v.Receipts[seqKey]; ok {
			if existing.LedgerID == ledgerID && existing.EntryID == entryID {
				return nil
			}
		}
		v.Receipts[seqKey] = MsgID{LedgerID: ledgerID, EntryID: entryID}
		if seq > v.LastSeq {
			v.LastSeq = seq
		}
		// trim window ~1024
		if len(v.Receipts) > 1024 {
			v.Receipts = map[string]MsgID{seqKey: v.Receipts[seqKey]}
		}
		_, err = casJSON(ctx, s.Raw, key, expect, v)
		if errors.Is(err, ErrVersionConflict) {
			continue
		}
		return err
	}
	return ErrVersionConflict
}

func (s *Store) LookupIdem(ctx context.Context, tenant, ns, topic, producer string, seq uint64) (MsgID, bool, error) {
	rec, err := getJSON[IdemValue](ctx, s.Raw, IdemKey(tenant, ns, topic, producer))
	if errors.Is(err, ErrNotFound) {
		return MsgID{}, false, nil
	}
	if err != nil {
		return MsgID{}, false, err
	}
	if rec.Value.Receipts == nil {
		return MsgID{}, false, nil
	}
	m, ok := rec.Value.Receipts[itoa(seq)]
	return m, ok, nil
}

func itoa(u uint64) string {
	if u == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for u > 0 {
		i--
		b[i] = byte('0' + u%10)
		u /= 10
	}
	return string(b[i:])
}
