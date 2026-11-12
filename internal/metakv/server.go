package metakv

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

type entry struct {
	Value   []byte
	Version uint64
}

// Server is a shared HTTP CAS KV for local k8s (OM + optional OL meta adapter).
type Server struct {
	mu   sync.Mutex
	data map[string]entry
}

func NewServer() *Server {
	return &Server{data: map[string]entry{}}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/kv/", s.handleKV)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}

type kvResp struct {
	Value   string `json:"value"` // base64
	Version uint64 `json:"version"`
}

func (s *Server) handleKV(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/kv/")
	if key == "" {
		http.Error(w, "missing key", 400)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		e, ok := s.data[key]
		s.mu.Unlock()
		if !ok {
			http.Error(w, "not found", 404)
			return
		}
		_ = json.NewEncoder(w).Encode(kvResp{Value: base64.StdEncoding.EncodeToString(e.Value), Version: e.Version})
	case http.MethodPut:
		expect, _ := strconv.ParseUint(r.Header.Get("X-Expect-Version"), 10, 64)
		var body struct {
			Value string `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		raw, err := base64.StdEncoding.DecodeString(body.Value)
		if err != nil {
			http.Error(w, "bad value", 400)
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		cur, ok := s.data[key]
		if !ok {
			if expect != 0 {
				http.Error(w, "conflict", 409)
				return
			}
			s.data[key] = entry{Value: raw, Version: 1}
			_ = json.NewEncoder(w).Encode(kvResp{Version: 1})
			return
		}
		if cur.Version != expect {
			http.Error(w, "conflict", 409)
			return
		}
		nv := cur.Version + 1
		s.data[key] = entry{Value: raw, Version: nv}
		_ = json.NewEncoder(w).Encode(kvResp{Version: nv})
	case http.MethodDelete:
		expect, _ := strconv.ParseUint(r.Header.Get("X-Expect-Version"), 10, 64)
		s.mu.Lock()
		defer s.mu.Unlock()
		cur, ok := s.data[key]
		if !ok {
			http.Error(w, "not found", 404)
			return
		}
		if cur.Version != expect {
			http.Error(w, "conflict", 409)
			return
		}
		delete(s.data, key)
		w.WriteHeader(204)
	default:
		http.Error(w, "method", 405)
	}
}
