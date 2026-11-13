package broker

import (
	"context"
	"sync"
	"time"

	"github.com/devotionalist/open-message/internal/metadata"
)

// cursorFlusher coalesces ack positions and periodically CasAdvances (F1.3).
type cursorFlusher struct {
	meta     *metadata.Store
	mu       sync.Mutex
	pend     map[string]metadata.Position // key tenant/ns/topic/sub
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

func newCursorFlusher(meta *metadata.Store) *cursorFlusher {
	f := &cursorFlusher{
		meta: meta, pend: map[string]metadata.Position{},
		stop: make(chan struct{}), done: make(chan struct{}),
	}
	go f.loop()
	return f
}

func (f *cursorFlusher) key(tenant, ns, topic, sub string) string {
	return tenant + "/" + ns + "/" + topic + "/" + sub
}

func (f *cursorFlusher) Enqueue(tenant, ns, topic, sub string, pos metadata.Position) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := f.key(tenant, ns, topic, sub)
	if cur, ok := f.pend[k]; ok {
		if positionAfter(pos, cur) {
			f.pend[k] = pos
		}
		return
	}
	f.pend[k] = pos
}

func positionAfter(a, b metadata.Position) bool {
	if a.LedgerID != b.LedgerID {
		return a.LedgerID > b.LedgerID
	}
	return a.EntryID > b.EntryID
}

func (f *cursorFlusher) loop() {
	defer close(f.done)
	t := time.NewTicker(50 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-f.stop:
			f.flushAll()
			return
		case <-t.C:
			f.flushAll()
		}
	}
}

// FlushNow drains pending cursor advances synchronously (Drain/Unload).
func (f *cursorFlusher) FlushNow() {
	f.flushAll()
}

func (f *cursorFlusher) flushAll() {
	f.mu.Lock()
	batch := f.pend
	f.pend = map[string]metadata.Position{}
	f.mu.Unlock()
	ctx := context.Background()
	for k, pos := range batch {
		// k = tenant/ns/topic/sub
		var tenant, ns, topic, sub string
		parts := split4(k)
		if len(parts) != 4 {
			continue
		}
		tenant, ns, topic, sub = parts[0], parts[1], parts[2], parts[3]
		_ = f.meta.CasAdvance(ctx, tenant, ns, topic, sub, pos)
	}
}

func (f *cursorFlusher) Stop() {
	f.stopOnce.Do(func() { close(f.stop) })
	<-f.done
}

func split4(s string) []string {
	out := make([]string, 0, 4)
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			out = append(out, s[start:i])
			start = i + 1
			if len(out) == 3 {
				out = append(out, s[start:])
				return out
			}
		}
	}
	return nil
}
