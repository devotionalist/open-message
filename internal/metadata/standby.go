package metadata

import "context"

// StandbyInfo is optional warm-standby assignment on OwnershipRecord.
type StandbyInfo struct {
	BrokerID   string `json:"brokerId"`
	BrokerAddr string `json:"brokerAddr"`
	SyncEpoch  uint64 `json:"syncEpoch"`
	Ready      bool   `json:"ready"`
	AssignedAt int64  `json:"assignedAt"`
}

// AssignStandby CAS-es standby onto an owned bundle.
func (s *Store) AssignStandby(ctx context.Context, ns, bundleID, standbyID, standbyAddr string) error {
	key := OwnershipKey(ns, bundleID)
	rec, err := getJSON[OwnershipRecord](ctx, s.Raw, key)
	if err != nil {
		return err
	}
	if rec.Value.BrokerID == "" {
		return ErrCasRejected
	}
	now := s.Clock.NowUnixMilli()
	next := rec.Value
	next.Standby = &StandbyInfo{
		BrokerID: standbyID, BrokerAddr: standbyAddr,
		SyncEpoch: rec.Value.Epoch, Ready: true, AssignedAt: now,
	}
	_, err = casJSON(ctx, s.Raw, key, rec.Version, next)
	return err
}

// PromoteStandby makes standby the primary (early promote; caller still must Fence).
func (s *Store) PromoteStandby(ctx context.Context, ns, bundleID, standbyID string, leaseTTLMs int64) (OwnershipRecord, error) {
	key := OwnershipKey(ns, bundleID)
	rec, err := getJSON[OwnershipRecord](ctx, s.Raw, key)
	if err != nil {
		return OwnershipRecord{}, err
	}
	if rec.Value.Standby == nil || rec.Value.Standby.BrokerID != standbyID || !rec.Value.Standby.Ready {
		return OwnershipRecord{}, ErrCasRejected
	}
	now := s.Clock.NowUnixMilli()
	next := OwnershipRecord{
		BrokerID:      standbyID,
		BrokerAddr:    rec.Value.Standby.BrokerAddr,
		Epoch:         rec.Value.Epoch + 1,
		LeaseExpireAt: now + leaseTTLMs,
		LastRenewAt:   now,
	}
	_, err = casJSON(ctx, s.Raw, key, rec.Version, next)
	if err != nil {
		return OwnershipRecord{}, err
	}
	return next, nil
}

// ClearStandby removes standby assignment.
func (s *Store) ClearStandby(ctx context.Context, ns, bundleID string) error {
	key := OwnershipKey(ns, bundleID)
	rec, err := getJSON[OwnershipRecord](ctx, s.Raw, key)
	if err != nil {
		return err
	}
	next := rec.Value
	next.Standby = nil
	_, err = casJSON(ctx, s.Raw, key, rec.Version, next)
	return err
}
