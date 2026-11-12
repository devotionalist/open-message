package olclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	ledgerclient "github.com/devotionalist/open-ledger-client-go"
)

// HTTPMetaStore stores LedgerMeta JSON in metakv under led/{id}.
type HTTPMetaStore struct {
	Base   string
	Client *http.Client
}

func (s *HTTPMetaStore) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func (s *HTTPMetaStore) url(id uint64) string {
	return strings.TrimRight(s.Base, "/") + "/kv/led/" + strconv.FormatUint(id, 10)
}

type kvWire struct {
	Value   string `json:"value"`
	Version uint64 `json:"version"`
}

func (s *HTTPMetaStore) Get(ledgerID uint64) (ledgerclient.LedgerMeta, error) {
	resp, err := s.client().Get(s.url(ledgerID))
	if err != nil {
		return ledgerclient.LedgerMeta{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return ledgerclient.LedgerMeta{}, &ledgerclient.OpError{Kind: ledgerclient.KindNotFound, LedgerID: ledgerID}
	}
	var wrap kvWire
	if err := json.NewDecoder(resp.Body).Decode(&wrap); err != nil {
		return ledgerclient.LedgerMeta{}, err
	}
	raw, err := base64.StdEncoding.DecodeString(wrap.Value)
	if err != nil {
		return ledgerclient.LedgerMeta{}, err
	}
	var meta ledgerclient.LedgerMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return ledgerclient.LedgerMeta{}, err
	}
	meta.MetaVersion = wrap.Version
	return meta, nil
}

func (s *HTTPMetaStore) put(expect uint64, meta ledgerclient.LedgerMeta) (ledgerclient.LedgerMeta, int, error) {
	b, err := json.Marshal(meta)
	if err != nil {
		return ledgerclient.LedgerMeta{}, 0, err
	}
	body, _ := json.Marshal(map[string]string{"value": base64.StdEncoding.EncodeToString(b)})
	req, err := http.NewRequest(http.MethodPut, s.url(meta.LedgerID), bytes.NewReader(body))
	if err != nil {
		return ledgerclient.LedgerMeta{}, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Expect-Version", strconv.FormatUint(expect, 10))
	resp, err := s.client().Do(req)
	if err != nil {
		return ledgerclient.LedgerMeta{}, 0, err
	}
	defer resp.Body.Close()
	code := resp.StatusCode
	if code != 200 {
		msg, _ := io.ReadAll(resp.Body)
		return ledgerclient.LedgerMeta{}, code, fmt.Errorf("metakv: %s %s", resp.Status, msg)
	}
	var wrap kvWire
	_ = json.NewDecoder(resp.Body).Decode(&wrap)
	meta.MetaVersion = wrap.Version
	return meta, code, nil
}

func (s *HTTPMetaStore) Create(meta ledgerclient.LedgerMeta) (ledgerclient.LedgerMeta, error) {
	out, code, err := s.put(0, meta)
	if code == 409 {
		return ledgerclient.LedgerMeta{}, &ledgerclient.OpError{Kind: ledgerclient.KindAlreadyExists, LedgerID: meta.LedgerID}
	}
	return out, err
}

func (s *HTTPMetaStore) CompareAndSwap(expectedVersion uint64, meta ledgerclient.LedgerMeta) (ledgerclient.LedgerMeta, error) {
	out, code, err := s.put(expectedVersion, meta)
	if code == 409 {
		return ledgerclient.LedgerMeta{}, &ledgerclient.OpError{Kind: ledgerclient.KindVersionConflict, LedgerID: meta.LedgerID}
	}
	if code == 404 {
		return ledgerclient.LedgerMeta{}, &ledgerclient.OpError{Kind: ledgerclient.KindNotFound, LedgerID: meta.LedgerID}
	}
	return out, err
}

// DialBookies opens gRPC bookies from addr list.
// BookieID is the first DNS label when present (bookie-0.bookie....), else bookie-N by index.
func DialBookies(ctx context.Context, addrs []string) (map[ledgerclient.BookieID]ledgerclient.Bookie, []*ledgerclient.GRPCBookie, error) {
	out := map[ledgerclient.BookieID]ledgerclient.Bookie{}
	var closers []*ledgerclient.GRPCBookie
	idx := 0
	for _, addr := range addrs {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		b, err := ledgerclient.DialBookie(ctx, addr)
		if err != nil {
			for _, c := range closers {
				_ = c.Close()
			}
			return nil, nil, err
		}
		id := bookieIDFromAddr(addr, idx)
		out[id] = b
		closers = append(closers, b)
		idx++
	}
	return out, closers, nil
}

func bookieIDFromAddr(addr string, idx int) ledgerclient.BookieID {
	host := addr
	if i := strings.LastIndex(addr, ":"); i > 0 && strings.Count(addr, ":") == 1 {
		host = addr[:i]
	}
	// DNS name bookie-0.bookie.... → bookie-0; bare IPs fall back to index.
	if !isLikelyIP(host) {
		if label, _, ok := strings.Cut(host, "."); ok && label != "" {
			return ledgerclient.BookieID(label)
		}
		if host != "" {
			return ledgerclient.BookieID(host)
		}
	}
	return ledgerclient.BookieID(fmt.Sprintf("bookie-%d", idx))
}

func isLikelyIP(host string) bool {
	if strings.Contains(host, ":") { // IPv6 or host:port mishandle
		return true
	}
	parts := strings.Split(host, ".")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		for _, c := range p {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

// NewRemote builds Adapter against shared HTTP LedgerMeta + gRPC bookies.
func NewRemote(metaURL string, bookies map[ledgerclient.BookieID]ledgerclient.Bookie, startID uint64) *Adapter {
	a := NewAdapter(&HTTPMetaStore{Base: metaURL}, bookies, startID)
	a.randomIDs = true
	return a
}
