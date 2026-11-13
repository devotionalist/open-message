package broker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/devotionalist/open-message/internal/clock"
	"github.com/devotionalist/open-message/internal/loadmgr"
	"github.com/devotionalist/open-message/internal/metadata"
	"github.com/devotionalist/open-message/internal/metrics"
	"github.com/devotionalist/open-message/internal/olclient"
	"github.com/devotionalist/open-message/internal/orphan"
	"github.com/devotionalist/open-message/internal/quota"
	"github.com/devotionalist/open-message/internal/topic"
)

// Config for a Broker process.
type Config struct {
	BrokerID      string
	Advertise     string
	ListenAddr    string
	AdminAddr     string
	AuthToken     string
	LeaseTTL      time.Duration
	RenewEvery    time.Duration
	RollEvery     time.Duration
	Quorum        olclient.QuorumPolicy
	PersistIdem   bool // M3 cross-owner idempotency
	MinBrokers    int
	MinBookies    int
}

func (c *Config) defaults() {
	if c.LeaseTTL == 0 {
		c.LeaseTTL = 15 * time.Second
	}
	if c.RenewEvery == 0 {
		c.RenewEvery = 3 * time.Second
	}
	if c.RollEvery == 0 {
		c.RollEvery = 0 // disabled unless set
	}
	if c.Quorum.E == 0 {
		c.Quorum = olclient.QuorumPolicy{E: 1, Qw: 1, Qa: 1}
	}
	if c.MinBrokers == 0 {
		c.MinBrokers = 1
	}
	if c.MinBookies == 0 {
		c.MinBookies = 1
	}
}

// Broker is the serving process.
type Broker struct {
	cfg    Config
	meta   *metadata.Store
	ol     olclient.Client
	log    *slog.Logger
	clk    clock.Clock
	quota  *quota.Limiter
	load   *loadmgr.Manager
	metrics *metrics.Counters
	flush  *cursorFlusher
	orphan *orphan.Reconciler
	sf     singleflight.Group

	mu       sync.Mutex
	status   string
	owned    map[string]ownedBundle
	topics   map[string]*topicRuntime
	seqDedup map[string]dedupEntry

	ln     net.Listener
	admin  *http.Server
	cancel context.CancelFunc
	wg     sync.WaitGroup

	inflight atomic.Int64 // in-flight Produce (Drain waits)
	accepting atomic.Bool
}

type ownedBundle struct {
	Namespace string
	BundleID  string
	Epoch     uint64
}

type dedupEntry struct {
	Seq      uint64
	LedgerID uint64
	EntryID  uint64
}

type topicRuntime struct {
	name   TopicName
	ml     *topic.ManagedLedger
	disp   *dispatcher
	bundle string
	epoch  uint64
}

func New(cfg Config, meta *metadata.Store, ol olclient.Client, log *slog.Logger) *Broker {
	cfg.defaults()
	if log == nil {
		log = slog.Default()
	}
	if meta.Clock == nil {
		meta.Clock = clock.System{}
	}
	m := &metrics.Counters{}
	b := &Broker{
		cfg: cfg, meta: meta, ol: ol, log: log, clk: meta.Clock,
		quota: quota.NewLimiter(), load: &loadmgr.Manager{Log: log, MaxTopicsHint: 100},
		metrics: m, flush: newCursorFlusher(meta),
		orphan: &orphan.Reconciler{Meta: meta, OL: ol, Log: log},
		status: "Serving", owned: map[string]ownedBundle{}, topics: map[string]*topicRuntime{},
		seqDedup: map[string]dedupEntry{},
	}
	b.quota.SetPolicy("default", quota.Policy{
		MinBrokers: cfg.MinBrokers, MinBookies: cfg.MinBookies,
	})
	b.accepting.Store(true)
	return b
}

func bundleKey(ns, bundle string) string { return ns + "/" + bundle }

func (b *Broker) Metrics() *metrics.Counters { return b.metrics }
func (b *Broker) LoadManager() *loadmgr.Manager { return b.load }
func (b *Broker) Quota() *quota.Limiter         { return b.quota }

func (b *Broker) Start(ctx context.Context) error {
	ctx, b.cancel = context.WithCancel(ctx)
	_ = b.meta.PutNode(ctx, b.cfg.BrokerID, metadata.BrokerNode{
		Addr: b.cfg.Advertise, Status: "Serving", LastHeartbeatAt: b.clk.NowUnixMilli(),
	})
	ln, err := net.Listen("tcp", b.cfg.ListenAddr)
	if err != nil {
		return err
	}
	b.ln = ln
	if b.cfg.AdminAddr != "" {
		mux := http.NewServeMux()
		b.mountAdmin(mux)
		b.admin = &http.Server{Addr: b.cfg.AdminAddr, Handler: mux}
		b.wg.Add(1)
		go func() {
			defer b.wg.Done()
			_ = b.admin.ListenAndServe()
		}()
	}
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		b.renewLoop(ctx)
	}()
	if b.cfg.RollEvery > 0 {
		b.wg.Add(1)
		go func() {
			defer b.wg.Done()
			b.rollLoop(ctx)
		}()
	}
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		b.acceptLoop(ctx)
	}()
	b.log.Info("broker started", "id", b.cfg.BrokerID, "listen", b.cfg.ListenAddr, "advertise", b.cfg.Advertise)
	return nil
}

func (b *Broker) Stop(ctx context.Context) error {
	_ = b.Drain(ctx)
	if b.cancel != nil {
		b.cancel()
	}
	if b.ln != nil {
		_ = b.ln.Close()
	}
	if b.admin != nil {
		_ = b.admin.Shutdown(ctx)
	}
	if b.flush != nil {
		b.flush.Stop()
	}
	b.wg.Wait()
	b.mu.Lock()
	b.status = "Stopped"
	b.mu.Unlock()
	stopCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = b.meta.PutNode(stopCtx, b.cfg.BrokerID, metadata.BrokerNode{
		Addr: b.cfg.Advertise, Status: "Stopped", LastHeartbeatAt: b.clk.NowUnixMilli(),
	})
	return nil
}

// Drain rejects new acquires/conns, waits in-flight Produce, flushes cursors, Unloads all bundles
// (design/09 §2–3.3, design/30 §2.2).
func (b *Broker) Drain(ctx context.Context) error {
	b.mu.Lock()
	if b.status == "Draining" || b.status == "Stopped" {
		b.mu.Unlock()
		return nil
	}
	b.status = "Draining"
	bundles := make([]ownedBundle, 0, len(b.owned))
	for _, o := range b.owned {
		bundles = append(bundles, o)
	}
	b.mu.Unlock()

	b.accepting.Store(false)
	if b.ln != nil {
		_ = b.ln.Close() // 拒新连；已有 conn 继续排空
	}
	_ = b.meta.PutNode(ctx, b.cfg.BrokerID, metadata.BrokerNode{
		Addr: b.cfg.Advertise, Status: "Draining", LastHeartbeatAt: b.clk.NowUnixMilli(),
	})

	b.waitInflight(ctx)
	if b.flush != nil {
		b.flush.FlushNow()
	}
	for _, o := range bundles {
		if err := b.UnloadBundle(ctx, o.Namespace, o.BundleID); err != nil {
			b.log.Warn("unload failed", "err", err)
		}
	}
	return nil
}

func (b *Broker) waitInflight(ctx context.Context) {
	t := time.NewTicker(5 * time.Millisecond)
	defer t.Stop()
	for {
		if b.inflight.Load() == 0 {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (b *Broker) renewLoop(ctx context.Context) {
	t := time.NewTicker(b.cfg.RenewEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b.mu.Lock()
			owned := make([]ownedBundle, 0, len(b.owned))
			for _, o := range b.owned {
				owned = append(owned, o)
			}
			status := b.status
			topicN := len(b.topics)
			b.mu.Unlock()
			b.load.Observe(b.cfg.BrokerID, topicN)
			_ = b.meta.PutNode(ctx, b.cfg.BrokerID, metadata.BrokerNode{
				Addr: b.cfg.Advertise, Status: status, LastHeartbeatAt: b.clk.NowUnixMilli(),
			})
			for _, o := range owned {
				if _, err := b.meta.Renew(ctx, o.Namespace, o.BundleID, b.cfg.BrokerID, o.Epoch, b.cfg.LeaseTTL.Milliseconds()); err != nil {
					b.log.Warn("renew failed; fencing local", "bundle", o.BundleID, "err", err)
					b.metrics.Failover.Add(1)
					_ = b.UnloadBundle(ctx, o.Namespace, o.BundleID)
				}
			}
		}
	}
}

func (b *Broker) rollLoop(ctx context.Context) {
	t := time.NewTicker(b.cfg.RollEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b.mu.Lock()
			rts := make([]*topicRuntime, 0, len(b.topics))
			for _, rt := range b.topics {
				rts = append(rts, rt)
			}
			b.mu.Unlock()
			for _, rt := range rts {
				if err := rt.ml.Roll(ctx); err != nil {
					b.log.Warn("periodic roll", "topic", rt.name.String(), "err", err)
					continue
				}
				b.metrics.Rolls.Add(1)
			}
		}
	}
}

func (b *Broker) EnsureOwner(ctx context.Context, tn TopicName) (uint64, error) {
	b.mu.Lock()
	if b.status != "Serving" {
		b.mu.Unlock()
		return 0, errors.New("draining")
	}
	bk := bundleKey(tn.Namespace, tn.BundleID())
	if o, ok := b.owned[bk]; ok {
		b.mu.Unlock()
		return o.Epoch, nil
	}
	b.mu.Unlock()

	rec, _, err := b.meta.TryAcquire(ctx, tn.Namespace, tn.BundleID(), b.cfg.BrokerID, b.cfg.Advertise, b.cfg.LeaseTTL.Milliseconds())
	if err != nil {
		if errors.Is(err, metadata.ErrCasRejected) {
			b.metrics.NotOwner.Add(1)
			return 0, fmt.Errorf("not owner: held by %s", rec.BrokerID)
		}
		return 0, err
	}
	b.mu.Lock()
	b.owned[bk] = ownedBundle{Namespace: tn.Namespace, BundleID: tn.BundleID(), Epoch: rec.Epoch}
	b.mu.Unlock()
	return rec.Epoch, nil
}

func (b *Broker) getOrLoadTopic(ctx context.Context, tn TopicName) (*topicRuntime, error) {
	epoch, err := b.EnsureOwner(ctx, tn)
	if err != nil {
		return nil, err
	}
	key := tn.String()
	b.mu.Lock()
	if rt, ok := b.topics[key]; ok && rt.ml.Ready() {
		b.mu.Unlock()
		return rt, nil
	}
	b.mu.Unlock()

	v, err, _ := b.sf.Do(key, func() (any, error) {
		b.mu.Lock()
		if rt, ok := b.topics[key]; ok && rt.ml.Ready() {
			b.mu.Unlock()
			return rt, nil
		}
		b.mu.Unlock()
		ml := topic.NewManagedLedger(tn.Tenant, tn.Namespace, tn.Topic, b.meta, b.ol, b.cfg.Quorum)
		if err := ml.Load(ctx); err != nil {
			return nil, err
		}
		rt := &topicRuntime{
			name: tn, ml: ml, disp: newDispatcher(ml, b.meta, tn, "Exclusive"),
			bundle: tn.BundleID(), epoch: epoch,
		}
		b.mu.Lock()
		b.topics[key] = rt
		b.mu.Unlock()
		return rt, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*topicRuntime), nil
}

func (b *Broker) UnloadBundle(ctx context.Context, ns, bundleID string) error {
	bk := bundleKey(ns, bundleID)
	b.mu.Lock()
	o, ok := b.owned[bk]
	var toClose []*topicRuntime
	for k, rt := range b.topics {
		if rt.bundle == bundleID && rt.name.Namespace == ns {
			toClose = append(toClose, rt)
			delete(b.topics, k)
		}
	}
	if ok {
		delete(b.owned, bk)
	}
	b.mu.Unlock()

	// 1) 拒新写 2) 等在途 3) 停分发 + Close ledger + flush 4) Unload CAS
	for _, rt := range toClose {
		rt.ml.FenceLocal()
	}
	b.waitInflight(ctx)
	if b.flush != nil {
		b.flush.FlushNow()
	}
	for _, rt := range toClose {
		rt.disp.stop()
		if err := rt.ml.CloseForUnload(ctx); err != nil {
			b.log.Warn("close for unload", "topic", rt.name.String(), "err", err)
			rt.ml.FenceLocal()
		}
	}
	if b.flush != nil {
		b.flush.FlushNow()
	}
	if ok {
		return b.meta.Unload(ctx, ns, bundleID, b.cfg.BrokerID, o.Epoch)
	}
	return nil
}

func (b *Broker) Lookup(ctx context.Context, topicStr string) (string, error) {
	tn, err := ParseTopic(topicStr)
	if err != nil {
		return "", err
	}
	rec, err := b.meta.Lookup(ctx, tn.Namespace, tn.BundleID())
	if err != nil {
		if errors.Is(err, metadata.ErrNotFound) {
			if _, aerr := b.EnsureOwner(ctx, tn); aerr == nil {
				return b.cfg.Advertise, nil
			}
			return "", err
		}
		return "", err
	}
	if rec.BrokerID == "" || b.clk.NowUnixMilli() > rec.LeaseExpireAt {
		if _, aerr := b.EnsureOwner(ctx, tn); aerr == nil {
			return b.cfg.Advertise, nil
		}
	}
	if rec.BrokerAddr == "" {
		return b.cfg.Advertise, nil
	}
	return rec.BrokerAddr, nil
}

// Produce path used by wire handler and in-process tests.
func (b *Broker) Produce(ctx context.Context, topicStr, producerName string, sequenceID uint64, idempotent bool, payload []byte) (ledgerID, entryID uint64, err error) {
	tn, err := ParseTopic(topicStr)
	if err != nil {
		return 0, 0, err
	}
	if !b.quota.AllowPublish(tn.Namespace, len(payload)) || !b.quota.BacklogOK(tn.Namespace) {
		b.metrics.ProduceErr.Add(1)
		return 0, 0, errors.New("quota exceeded")
	}
	// inflight 先于 status 检查，避免 Drain waitInflight 漏掉刚通过检查的写。
	b.inflight.Add(1)
	defer b.inflight.Add(-1)
	b.mu.Lock()
	st := b.status
	b.mu.Unlock()
	if st != "Serving" {
		b.metrics.ProduceErr.Add(1)
		return 0, 0, errors.New("draining")
	}
	rt, err := b.getOrLoadTopic(ctx, tn)
	if err != nil {
		b.metrics.ProduceErr.Add(1)
		return 0, 0, err
	}
	if !rt.ml.Ready() {
		b.metrics.ProduceErr.Add(1)
		return 0, 0, errors.New("not ready")
	}
	if idempotent && producerName != "" {
		if b.cfg.PersistIdem {
			if m, ok, lerr := b.meta.LookupIdem(ctx, tn.Tenant, tn.Namespace, tn.Topic, producerName, sequenceID); lerr == nil && ok {
				b.metrics.IdempotentHit.Add(1)
				return m.LedgerID, m.EntryID, nil
			}
		}
		b.mu.Lock()
		if d, ok := b.seqDedup[producerName+"/"+strconv.FormatUint(sequenceID, 10)]; ok {
			lid, eid := d.LedgerID, d.EntryID
			b.mu.Unlock()
			b.metrics.IdempotentHit.Add(1)
			return lid, eid, nil
		}
		b.mu.Unlock()
	}
	lid, eid, err := rt.ml.Append(ctx, payload)
	if err != nil {
		// ManagedLedger already attempts same-owner roll on Fenced/Unavailable.
		b.metrics.ProduceErr.Add(1)
		return 0, 0, err
	}
	b.metrics.ProduceOK.Add(1)
	b.quota.AddBacklog(tn.Namespace, int64(len(payload)))
	if idempotent && producerName != "" {
		b.mu.Lock()
		sk := producerName + "/" + strconv.FormatUint(sequenceID, 10)
		b.seqDedup[sk] = dedupEntry{Seq: sequenceID, LedgerID: lid, EntryID: eid}
		if len(b.seqDedup) > 4096 {
			b.seqDedup = map[string]dedupEntry{sk: {Seq: sequenceID, LedgerID: lid, EntryID: eid}}
		}
		b.mu.Unlock()
		if b.cfg.PersistIdem {
			_ = b.meta.PutIdemReceipt(ctx, tn.Tenant, tn.Namespace, tn.Topic, producerName, sequenceID, lid, eid)
		}
	}
	rt.disp.notify()
	return lid, eid, nil
}

func (b *Broker) IsOwnerOf(topicStr string) bool {
	tn, err := ParseTopic(topicStr)
	if err != nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.owned[bundleKey(tn.Namespace, tn.BundleID())]
	return ok
}

// AssignStandby registers this broker as warm standby for a bundle.
func (b *Broker) AssignStandby(ctx context.Context, ns, bundleID string) error {
	return b.meta.AssignStandby(ctx, ns, bundleID, b.cfg.BrokerID, b.cfg.Advertise)
}

// PromoteStandby early-promotes and loads topics (still Fence via Load).
func (b *Broker) PromoteStandby(ctx context.Context, ns, bundleID string) error {
	rec, err := b.meta.PromoteStandby(ctx, ns, bundleID, b.cfg.BrokerID, b.cfg.LeaseTTL.Milliseconds())
	if err != nil {
		return err
	}
	b.mu.Lock()
	b.owned[bundleKey(ns, bundleID)] = ownedBundle{Namespace: ns, BundleID: bundleID, Epoch: rec.Epoch}
	b.mu.Unlock()
	b.metrics.Failover.Add(1)
	return nil
}

// EnsurePartitionedTopic creates N physical partitions (F2.8).
func (b *Broker) EnsurePartitionedTopic(ctx context.Context, base string, n int) error {
	tn, err := ParseTopic(base)
	if err != nil {
		return err
	}
	for i := 0; i < n; i++ {
		pt := TopicName{Tenant: tn.Tenant, Namespace: tn.Namespace, Topic: fmt.Sprintf("%s-partition-%d", tn.Topic, i)}
		if _, err := b.getOrLoadTopic(ctx, pt); err != nil {
			return err
		}
	}
	return nil
}

// ReconcileOrphans runs full orphan mark for candidates (F2.6).
func (b *Broker) ReconcileOrphans(ctx context.Context, topicStr string, candidates []uint64) (orphan.Report, error) {
	tn, err := ParseTopic(topicStr)
	if err != nil {
		return orphan.Report{}, err
	}
	rep, err := b.orphan.ReconcileTopic(ctx, tn.Tenant, tn.Namespace, tn.Topic, candidates)
	if err == nil {
		b.metrics.OrphanMarked.Add(uint64(rep.Marked))
	}
	return rep, err
}

// CanScaleIn enforces min brokers isolation guard (F3.5).
func (b *Broker) CanScaleIn(ns string, brokersAfter int) bool {
	p := b.quota.Policy(ns)
	if p.MinBrokers == 0 {
		p.MinBrokers = b.cfg.MinBrokers
	}
	return quota.AllowScaleIn(p, brokersAfter)
}

