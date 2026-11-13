package proxy

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/devotionalist/open-message/internal/wire"
)

// Config for Proxy.
type Config struct {
	ListenAddr    string
	DefaultBroker string // bootstrap; comma-separated for dial failover
	// ShutdownGrace is how long Stop waits for in-flight client pipes (default 15s).
	ShutdownGrace time.Duration
}

// Proxy terminates OM frames, Lookup, and forwards to an available broker.
type Proxy struct {
	cfg Config
	log *slog.Logger
	ln  net.Listener

	mu       sync.Mutex
	cache    map[string]string // topic -> broker (reserved)
	bootList []string

	connWg   sync.WaitGroup
	stopping bool
}

func New(cfg Config, log *slog.Logger) *Proxy {
	if log == nil {
		log = slog.Default()
	}
	if cfg.ShutdownGrace <= 0 {
		cfg.ShutdownGrace = 15 * time.Second
	}
	var boots []string
	for _, p := range strings.Split(cfg.DefaultBroker, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			boots = append(boots, p)
		}
	}
	if len(boots) == 0 {
		boots = []string{"127.0.0.1:6651"}
	}
	return &Proxy{cfg: cfg, log: log, cache: map[string]string{}, bootList: boots}
}

func (p *Proxy) Start(ctx context.Context) error {
	ln, err := net.Listen("tcp", p.cfg.ListenAddr)
	if err != nil {
		return err
	}
	p.ln = ln
	go p.accept(ctx)
	p.log.Info("proxy started", "listen", p.cfg.ListenAddr, "brokers", p.bootList)
	return nil
}

// Stop closes the listener (reject new clients) and waits for in-flight pipes
// up to ShutdownGrace, then returns (remaining conns are closed by callers).
func (p *Proxy) Stop() error {
	p.mu.Lock()
	p.stopping = true
	p.mu.Unlock()
	if p.ln != nil {
		_ = p.ln.Close()
	}
	done := make(chan struct{})
	go func() {
		p.connWg.Wait()
		close(done)
	}()
	t := time.NewTimer(p.cfg.ShutdownGrace)
	defer t.Stop()
	select {
	case <-done:
		p.log.Info("proxy stopped (drained)")
	case <-t.C:
		p.log.Warn("proxy stop grace elapsed; some connections still open")
	}
	return nil
}

func (p *Proxy) accept(ctx context.Context) {
	for {
		c, err := p.ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				return
			}
		}
		p.mu.Lock()
		stopping := p.stopping
		p.mu.Unlock()
		if stopping {
			_ = c.Close()
			continue
		}
		p.connWg.Add(1)
		go func() {
			defer p.connWg.Done()
			p.serveClient(ctx, c)
		}()
	}
}

func (p *Proxy) dialBootstrap() (net.Conn, string, error) {
	var last error
	n := len(p.bootList)
	start := int(time.Now().UnixNano() % int64(n))
	for i := 0; i < n; i++ {
		addr := p.bootList[(start+i)%n]
		c, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err == nil {
			return c, addr, nil
		}
		last = err
	}
	return nil, "", last
}

func (p *Proxy) serveClient(ctx context.Context, client net.Conn) {
	defer client.Close()
	upstream, addr, err := p.dialBootstrap()
	if err != nil {
		p.log.Warn("bootstrap dial failed", "err", err)
		return
	}
	defer upstream.Close()
	p.log.Debug("client bound", "broker", addr)

	errCh := make(chan error, 2)
	go func() { errCh <- p.clientToUpstream(ctx, client, upstream) }()
	go func() { errCh <- pipe(upstream, client) }()
	<-errCh
}

func pipe(src, dst net.Conn) error {
	_, err := io.Copy(dst, src)
	return err
}

func (p *Proxy) clientToUpstream(ctx context.Context, client net.Conn, upstream net.Conn) error {
	_ = ctx
	for {
		fr, err := wire.Decode(client)
		if err != nil {
			return err
		}
		if fr.Header.Type == wire.TypeLookup {
			body, _ := wire.DecodeBody[wire.LookupBody](fr.Header)
			_ = body
		}
		buf, err := wire.Encode(fr.Header, fr.Payload)
		if err != nil {
			return err
		}
		if _, err := upstream.Write(buf); err != nil {
			return err
		}
	}
}
