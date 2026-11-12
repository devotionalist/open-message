package metakv

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/devotionalist/open-message/internal/metadata"
)

// HTTPStore implements metadata.RawStore over metakv Server.
type HTTPStore struct {
	Base   string
	Client *http.Client
	Prefix string
}

func (s *HTTPStore) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return http.DefaultClient
}

func (s *HTTPStore) keyURL(key string) string {
	p := s.Prefix + key
	return strings.TrimRight(s.Base, "/") + "/kv/" + url.PathEscape(p)
}

func (s *HTTPStore) Get(ctx context.Context, key string) ([]byte, uint64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.keyURL(key), nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := s.client().Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, 0, metadata.ErrNotFound
	}
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, 0, fmt.Errorf("metakv get: %s %s", resp.Status, b)
	}
	var out kvResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, 0, err
	}
	raw, err := base64.StdEncoding.DecodeString(out.Value)
	return raw, out.Version, err
}

func (s *HTTPStore) Cas(ctx context.Context, key string, expectVersion uint64, value []byte) (uint64, error) {
	body, _ := json.Marshal(map[string]string{"value": base64.StdEncoding.EncodeToString(value)})
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, s.keyURL(key), bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Expect-Version", fmt.Sprintf("%d", expectVersion))
	resp, err := s.client().Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 409 {
		return 0, metadata.ErrVersionConflict
	}
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("metakv cas: %s %s", resp.Status, b)
	}
	var out kvResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, err
	}
	return out.Version, nil
}

func (s *HTTPStore) Delete(ctx context.Context, key string, expectVersion uint64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, s.keyURL(key), nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Expect-Version", fmt.Sprintf("%d", expectVersion))
	resp, err := s.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return metadata.ErrNotFound
	}
	if resp.StatusCode == 409 {
		return metadata.ErrVersionConflict
	}
	if resp.StatusCode != 204 {
		return fmt.Errorf("metakv delete: %s", resp.Status)
	}
	return nil
}

func (s *HTTPStore) ListPrefix(ctx context.Context, prefix string) ([]string, error) {
	// Minimal: not supported over HTTP list — return empty (Node listing optional for M1).
	_ = ctx
	_ = prefix
	return nil, nil
}
