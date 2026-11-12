package metadata

// Position is a cursor / message id location.
type Position struct {
	LedgerID uint64 `json:"ledgerId"`
	EntryID  int64  `json:"entryId"`
}

// OwnershipRecord is the Primary (and optional Standby) for a bundle.
type OwnershipRecord struct {
	BrokerID       string       `json:"brokerId"`
	BrokerAddr     string       `json:"brokerAddr"`
	Epoch          uint64       `json:"epoch"`
	LeaseExpireAt  int64        `json:"leaseExpireAt"`
	LastRenewAt    int64        `json:"lastRenewAt,omitempty"`
	IsolationGroup string       `json:"isolationGroup,omitempty"`
	Standby        *StandbyInfo `json:"standby,omitempty"`
}

// LedgerIndexValue is the topic → ledger id chain (no LedgerMeta).
type LedgerIndexValue struct {
	LedgerIDs      []uint64 `json:"ledgerIds"`
	OpenLedgerID   *uint64  `json:"openLedgerId,omitempty"`
	OpenStateHint  string   `json:"openStateHint,omitempty"`
	TruncatedBefore *uint64 `json:"truncatedBefore,omitempty"`
}

// CursorValue is mark-delete cursor state.
type CursorValue struct {
	MarkDelete     Position `json:"markDelete"`
	IndividualAcks []Position `json:"individualAcks,omitempty"`
}

// BrokerNode is NodeStore payload.
type BrokerNode struct {
	Addr             string   `json:"addr"`
	Status           string   `json:"status"` // Serving|Draining|Stopped
	LastHeartbeatAt  int64    `json:"lastHeartbeatAt"`
	IsolationGroups  []string `json:"isolationGroups,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
}

// Record is a versioned KV envelope.
type Record[T any] struct {
	Value   T
	Version uint64
}
