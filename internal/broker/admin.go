package broker

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/devotionalist/open-message/internal/quota"
)

func (b *Broker) mountAdmin(mux *http.ServeMux) {
	mux.HandleFunc("/admin/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/metrics", b.metrics.Handler())
	mux.HandleFunc("/admin/v1/brokers/drain", b.withAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", 405)
			return
		}
		_ = b.Drain(r.Context())
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"draining"}`))
	}))
	mux.HandleFunc("/admin/v1/brokers/scale-in-check", b.withAuth(func(w http.ResponseWriter, r *http.Request) {
		ns := r.URL.Query().Get("ns")
		if ns == "" {
			ns = "default"
		}
		after, _ := strconv.Atoi(r.URL.Query().Get("after"))
		ok := b.CanScaleIn(ns, after)
		_ = json.NewEncoder(w).Encode(map[string]any{"allowed": ok, "minBrokers": b.quota.Policy(ns).MinBrokers})
		if !ok {
			w.WriteHeader(http.StatusConflict)
		}
	}))
	mux.HandleFunc("/admin/v1/namespaces/", b.withAuth(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/admin/v1/namespaces/"), "/")
		if len(parts) >= 4 && parts[1] == "bundles" && parts[3] == "unload" && r.Method == http.MethodPost {
			ns, bundle := parts[0], parts[2]
			if err := b.UnloadBundle(r.Context(), ns, bundle); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"unloaded":true}`))
			return
		}
		if len(parts) >= 4 && parts[1] == "bundles" && parts[3] == "assign-standby" && r.Method == http.MethodPost {
			if err := b.AssignStandby(r.Context(), parts[0], parts[2]); err != nil {
				http.Error(w, err.Error(), 409)
				return
			}
			_, _ = w.Write([]byte(`{"standby":true}`))
			return
		}
		if len(parts) >= 4 && parts[1] == "bundles" && parts[3] == "promote" && r.Method == http.MethodPost {
			if err := b.PromoteStandby(r.Context(), parts[0], parts[2]); err != nil {
				http.Error(w, err.Error(), 409)
				return
			}
			_, _ = w.Write([]byte(`{"promoted":true}`))
			return
		}
		if len(parts) >= 2 && parts[1] == "policies" && r.Method == http.MethodPut {
			var p quota.Policy
			if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			b.quota.SetPolicy(parts[0], p)
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		http.NotFound(w, r)
	}))
	mux.HandleFunc("/admin/v1/topics/", b.withAuth(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/admin/v1/topics/")
		if strings.HasSuffix(path, "/partitions") && r.Method == http.MethodPut {
			base := strings.TrimSuffix(path, "/partitions")
			n, _ := strconv.Atoi(r.URL.Query().Get("n"))
			if n <= 0 {
				n = 2
			}
			if err := b.EnsurePartitionedTopic(r.Context(), base, n); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"partitions": n})
			return
		}
		if strings.HasSuffix(path, "/orphans/reconcile") && r.Method == http.MethodPost {
			topic := strings.TrimSuffix(path, "/orphans/reconcile")
			var body struct {
				Candidates []uint64 `json:"candidates"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			rep, err := b.ReconcileOrphans(r.Context(), topic, body.Candidates)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			_ = json.NewEncoder(w).Encode(rep)
			return
		}
		http.NotFound(w, r)
	}))
	mux.HandleFunc("/admin/v1/load/alerts", b.withAuth(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(b.load.Alerts())
	}))
	mux.HandleFunc("/admin/v1/stats", b.withAuth(func(w http.ResponseWriter, _ *http.Request) {
		b.mu.Lock()
		defer b.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"brokerId": b.cfg.BrokerID,
			"status":   b.status,
			"owned":    len(b.owned),
			"topics":   len(b.topics),
			"metrics":  b.metrics.Snapshot(),
		})
	}))
}

func (b *Broker) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if b.cfg.AuthToken != "" {
			tok := r.Header.Get("Authorization")
			if tok != "Bearer "+b.cfg.AuthToken {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next(w, r)
	}
}
