package olclient

import (
	"fmt"

	ledgerclient "github.com/devotionalist/open-ledger-client-go"
)

// NewMemoryQuorum builds an in-process multi-bookie client (E/Qw/Qa).
func NewMemoryQuorum(e, qw, qa uint32) *Adapter {
	if e == 0 {
		e = 1
	}
	return NewMemoryBookiePool(e, e, qw, qa)
}

// NewMemoryBookiePool builds n in-process bookies; Create uses ensemble size e.
func NewMemoryBookiePool(n, e, qw, qa uint32) *Adapter {
	_ = qw
	_ = qa
	if n == 0 {
		n = 1
	}
	if e == 0 {
		e = 1
	}
	if e > n {
		e = n
	}
	metas := ledgerclient.NewMemoryMetaStore()
	bookies := map[ledgerclient.BookieID]ledgerclient.Bookie{}
	for i := uint32(0); i < n; i++ {
		id := ledgerclient.BookieID(fmt.Sprintf("b%d", i))
		bookies[id] = ledgerclient.NewMemoryBookie()
	}
	return NewAdapter(metas, bookies, 1)
}
