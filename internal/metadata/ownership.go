package metadata

import (
	"context"
	"errors"
)

// TryAcquire becomes Primary if missing, expired, or same broker renewing.
func (s *Store) TryAcquire(ctx context.Context, ns, bundleID, brokerID, brokerAddr string, leaseTTLMs int64) (OwnershipRecord, uint64, error) {
	key := OwnershipKey(ns, bundleID)
	now := s.Clock.NowUnixMilli()
	rec, err := getJSON[OwnershipRecord](ctx, s.Raw, key)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return OwnershipRecord{}, 0, err
	}
	expect := uint64(0)
	next := OwnershipRecord{
		BrokerID:      brokerID,
		BrokerAddr:    brokerAddr,
		Epoch:         1,
		LeaseExpireAt: now + leaseTTLMs,
		LastRenewAt:   now,
	}
	if err == nil {
		expect = rec.Version
		owned := rec.Value
		alive := owned.BrokerID != "" && now <= owned.LeaseExpireAt
		if alive && owned.BrokerID != brokerID {
			return owned, rec.Version, ErrCasRejected
		}
		next.Epoch = owned.Epoch + 1
		if owned.BrokerID == brokerID && alive {
			next.Epoch = owned.Epoch // renew same epoch
		}
	}
	ver, err := casJSON(ctx, s.Raw, key, expect, next)
	if err != nil {
		return OwnershipRecord{}, 0, err
	}
	return next, ver, nil
}

func (s *Store) Renew(ctx context.Context, ns, bundleID, brokerID string, epoch uint64, leaseTTLMs int64) (OwnershipRecord, error) {
	key := OwnershipKey(ns, bundleID)
	rec, err := getJSON[OwnershipRecord](ctx, s.Raw, key)
	if err != nil {
		return OwnershipRecord{}, err
	}
	if rec.Value.BrokerID != brokerID || rec.Value.Epoch != epoch {
		return OwnershipRecord{}, ErrCasRejected
	}
	now := s.Clock.NowUnixMilli()
	next := rec.Value
	next.LeaseExpireAt = now + leaseTTLMs
	next.LastRenewAt = now
	if _, err := casJSON(ctx, s.Raw, key, rec.Version, next); err != nil {
		return OwnershipRecord{}, err
	}
	return next, nil
}

func (s *Store) Unload(ctx context.Context, ns, bundleID, brokerID string, epoch uint64) error {
	key := OwnershipKey(ns, bundleID)
	rec, err := getJSON[OwnershipRecord](ctx, s.Raw, key)
	if err != nil {
		return err
	}
	if rec.Value.BrokerID != brokerID || rec.Value.Epoch != epoch {
		return ErrCasRejected
	}
	empty := OwnershipRecord{}
	_, err = casJSON(ctx, s.Raw, key, rec.Version, empty)
	return err
}

func (s *Store) Lookup(ctx context.Context, ns, bundleID string) (OwnershipRecord, error) {
	rec, err := getJSON[OwnershipRecord](ctx, s.Raw, OwnershipKey(ns, bundleID))
	if err != nil {
		return OwnershipRecord{}, err
	}
	return rec.Value, nil
}
