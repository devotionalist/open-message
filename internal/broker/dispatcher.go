package broker

import (
	"context"
	"hash/fnv"
	"sync"
	"time"

	"github.com/devotionalist/open-message/internal/metadata"
	"github.com/devotionalist/open-message/internal/topic"
)

type consumerState struct {
	id       uint64
	sub      string
	permits  int32
	ch       chan delivered
	read     metadata.Position
	mode     string
	started  bool
	name     string
	priority int
	keyRanges []hashRange // Key_Shared sticky
}

type hashRange struct{ From, To uint32 }

type delivered struct {
	LedgerID uint64
	EntryID  uint64
	Payload  []byte
}

// dispatcher supports Exclusive / Failover / Shared / Key_Shared.
type dispatcher struct {
	ml   *topic.ManagedLedger
	meta *metadata.Store
	tn   TopicName
	mode string

	mu       sync.Mutex
	cons     []*consumerState
	active   int // Failover active index
	rr       int // Shared round-robin
	stopCh   chan struct{}
	wake     chan struct{}
	pending  map[uint64]map[uint64]struct{} // Shared individual unacked ledger->entry set (simplified)
	stopOnce sync.Once
}

func newDispatcher(ml *topic.ManagedLedger, meta *metadata.Store, tn TopicName, mode string) *dispatcher {
	if mode == "" {
		mode = "Exclusive"
	}
	d := &dispatcher{
		ml: ml, meta: meta, tn: tn, mode: mode,
		stopCh: make(chan struct{}), wake: make(chan struct{}, 16),
		pending: map[uint64]map[uint64]struct{}{},
	}
	go d.loop()
	return d
}

func (d *dispatcher) stop() {
	d.stopOnce.Do(func() { close(d.stopCh) })
}

func (d *dispatcher) notify() {
	// Prefer not to drop: a lost wake after produce leaves consumers stuck until next Flow.
	select {
	case d.wake <- struct{}{}:
	default:
		// buffer full — force another attempt by draining one and re-sending
		select {
		case <-d.wake:
		default:
		}
		select {
		case d.wake <- struct{}{}:
		default:
		}
	}
}

func (d *dispatcher) subscribe(c *consumerState) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.cons) == 0 && c.mode != "" {
		d.mode = c.mode
	}
	switch d.mode {
	case "Exclusive":
		if len(d.cons) > 0 {
			return errBusy
		}
		d.cons = []*consumerState{c}
		d.active = 0
	case "Failover":
		d.cons = append(d.cons, c)
		d.pickActive()
	case "Shared", "Key_Shared":
		d.cons = append(d.cons, c)
	default:
		d.cons = []*consumerState{c}
	}
	d.notify()
	return nil
}

func (d *dispatcher) unsubscribe(id uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := d.cons[:0]
	for _, c := range d.cons {
		if c.id != id {
			out = append(out, c)
		}
	}
	d.cons = out
	if d.mode == "Failover" {
		d.pickActive()
	}
	d.notify()
}

func (d *dispatcher) pickActive() {
	if len(d.cons) == 0 {
		d.active = -1
		return
	}
	best := 0
	for i := 1; i < len(d.cons); i++ {
		if d.cons[i].priority < d.cons[best].priority {
			best = i
		}
	}
	d.active = best
}

func (d *dispatcher) flow(consumerID uint64, n int32) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, c := range d.cons {
		if c.id == consumerID {
			c.permits += n
			d.notify()
			return
		}
	}
}

func (d *dispatcher) ackCumulative(ctx context.Context, consumerID uint64, pos metadata.Position) error {
	d.mu.Lock()
	sub := ""
	for _, c := range d.cons {
		if c.id == consumerID {
			sub = c.sub
			break
		}
	}
	d.mu.Unlock()
	if sub == "" {
		return nil
	}
	return d.meta.CasAdvance(ctx, d.tn.Tenant, d.tn.Namespace, d.tn.Topic, sub, pos)
}

func (d *dispatcher) ackIndividual(ctx context.Context, consumerID uint64, pos metadata.Position) error {
	d.mu.Lock()
	sub := ""
	for _, c := range d.cons {
		if c.id == consumerID {
			sub = c.sub
			break
		}
	}
	if m := d.pending[pos.LedgerID]; m != nil {
		delete(m, uint64(pos.EntryID))
	}
	d.mu.Unlock()
	if sub == "" {
		return nil
	}
	return d.meta.CasAdvanceIndividual(ctx, d.tn.Tenant, d.tn.Namespace, d.tn.Topic, sub, pos)
}

var errBusy = errString("consumer busy")

type errString string

func (e errString) Error() string { return string(e) }

func (d *dispatcher) loop() {
	ctx := context.Background()
	// Periodic wake: retry reads after transient bookie outages without new produces.
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-d.stopCh:
			return
		case <-d.wake:
		case <-tick.C:
		}
		for d.dispatchOne(ctx) {
		}
		// Consume coalesced wakes so a produce notify during an empty pass is not lost.
		drained := false
		for {
			select {
			case <-d.wake:
				drained = true
			default:
				goto nextWait
			}
		}
	nextWait:
		if drained {
			for d.dispatchOne(ctx) {
			}
		}
	}
}

func (d *dispatcher) dispatchOne(ctx context.Context) bool {
	d.mu.Lock()
	if len(d.cons) == 0 {
		d.mu.Unlock()
		return false
	}
	var targets []*consumerState
	switch d.mode {
	case "Exclusive", "Failover":
		if d.active < 0 || d.active >= len(d.cons) {
			d.mu.Unlock()
			return false
		}
		c := d.cons[d.active]
		if c.permits <= 0 {
			d.mu.Unlock()
			return false
		}
		targets = []*consumerState{c}
	default:
		for _, c := range d.cons {
			if c.permits > 0 {
				targets = append(targets, c)
			}
		}
		if len(targets) == 0 {
			d.mu.Unlock()
			return false
		}
	}
	// pick consumer for Shared / Key_Shared later after reading entry
	readBase := d.cons[0]
	if d.mode == "Failover" || d.mode == "Exclusive" {
		readBase = d.cons[d.active]
	}
	read := readBase.read
	started := readBase.started
	d.mu.Unlock()

	ids, err := d.ml.LedgerIDs(ctx)
	if err != nil || len(ids) == 0 {
		return false
	}
	var lid uint64
	var eid uint64
	if !started {
		lid = ids[0]
		eid = 0
	} else {
		lid = read.LedgerID
		eid = uint64(read.EntryID + 1)
	}
	payload, err := d.ml.ReadEntry(ctx, lid, eid)
	if err != nil {
		// Only skip to the next ledger when past this ledger's LAC (last>=0 && eid>last).
		// Empty (last=-1) or mid-ledger read errors must wait/retry — never skip durable entries.
		last, lerr := d.ml.LastEntryID(ctx, lid)
		pastTip := lerr == nil && last >= 0 && int64(eid) > last
		if !pastTip {
			return false
		}
		found := false
		for i, id := range ids {
			if id == lid && i+1 < len(ids) {
				lid = ids[i+1]
				eid = 0
				payload, err = d.ml.ReadEntry(ctx, lid, eid)
				found = err == nil
				break
			}
		}
		if !found {
			return false
		}
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	var c *consumerState
	switch d.mode {
	case "Exclusive", "Failover":
		if d.active < 0 || d.active >= len(d.cons) || d.cons[d.active].permits <= 0 {
			return false
		}
		c = d.cons[d.active]
	case "Key_Shared":
		h := hashPayload(payload)
		c = d.pickKeyShared(h)
		if c == nil || c.permits <= 0 {
			return false
		}
	default: // Shared
		if len(d.cons) == 0 {
			return false
		}
		for i := 0; i < len(d.cons); i++ {
			idx := (d.rr + i) % len(d.cons)
			if d.cons[idx].permits > 0 {
				c = d.cons[idx]
				d.rr = idx + 1
				break
			}
		}
		if c == nil {
			return false
		}
	}
	c.permits--
	c.started = true
	c.read = metadata.Position{LedgerID: lid, EntryID: int64(eid)}
	// advance Exclusive/Failover cursor read on all for next
	if d.mode == "Exclusive" || d.mode == "Failover" {
		for _, x := range d.cons {
			x.started = true
			x.read = c.read
		}
	} else {
		// Shared: advance logical read on first consumer tracker
		if len(d.cons) > 0 {
			d.cons[0].started = true
			d.cons[0].read = c.read
		}
		if d.pending[lid] == nil {
			d.pending[lid] = map[uint64]struct{}{}
		}
		d.pending[lid][eid] = struct{}{}
	}
	ch := c.ch
	go func() {
		select {
		case ch <- delivered{LedgerID: lid, EntryID: eid, Payload: payload}:
		case <-d.stopCh:
		}
	}()
	return true
}

func (d *dispatcher) pickKeyShared(h uint32) *consumerState {
	for _, c := range d.cons {
		if len(c.keyRanges) == 0 {
			return c
		}
		for _, r := range c.keyRanges {
			if h >= r.From && h <= r.To {
				return c
			}
		}
	}
	if len(d.cons) == 0 {
		return nil
	}
	return d.cons[int(h)%len(d.cons)]
}

func hashPayload(b []byte) uint32 {
	h := fnv.New32a()
	_, _ = h.Write(b)
	return h.Sum32()
}
