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
}

// Proxy terminates OM frames, Lookup, and forwards to an available broker.
type Proxy struct {
	cfg Config
	log *slog.Logger
	ln  net.Listener

	mu       sync.Mutex
	cache    map[string]string // topic -> broker (reserved)
	bootList []string
}

func New(cfg Config, log *slog.Logger) *Proxy {
	if log == nil {
		log = slog.Default()
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

func (p *Proxy) Stop() error {
	if p.ln != nil {
		return p.ln.Close()
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
		go p.serveClient(ctx, c)
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
