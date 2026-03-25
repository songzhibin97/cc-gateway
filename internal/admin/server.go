package admin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/songzhibin97/cc-gateway/internal/accounting"
	"github.com/songzhibin97/cc-gateway/internal/domain"
	"github.com/songzhibin97/cc-gateway/internal/proxy"
	"github.com/songzhibin97/cc-gateway/internal/router"
	"github.com/songzhibin97/cc-gateway/internal/store"
)

type Server struct {
	router      *router.Router
	keyStore    *proxy.KeyStore
	accountRepo *store.AccountRepo
	groupRepo   *store.GroupRepo
	keyRepo     *store.APIKeyRepo
	logRepo     *store.LogRepo
	cleaner     *accounting.Cleaner
	reloadFn    func() error
	adminToken  string
	logger      *slog.Logger
}

type Config struct {
	Router      *router.Router
	KeyStore    *proxy.KeyStore
	AccountRepo *store.AccountRepo
	GroupRepo   *store.GroupRepo
	KeyRepo     *store.APIKeyRepo
	LogRepo     *store.LogRepo
	Cleaner     *accounting.Cleaner
	ReloadFn    func() error
	AdminToken  string
	Logger      *slog.Logger
}

func NewServer(cfg Config) *Server {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.ReloadFn == nil {
		cfg.ReloadFn = func() error { return nil }
	}

	return &Server{
		router:      cfg.Router,
		keyStore:    cfg.KeyStore,
		accountRepo: cfg.AccountRepo,
		groupRepo:   cfg.GroupRepo,
		keyRepo:     cfg.KeyRepo,
		logRepo:     cfg.LogRepo,
		cleaner:     cfg.Cleaner,
		reloadFn:    cfg.ReloadFn,
		adminToken:  cfg.AdminToken,
		logger:      logger,
	}
}

func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()

	r.Handle("/metrics", promhttp.Handler())

	r.Route("/admin", func(r chi.Router) {
		if s.adminToken != "" {
			r.Use(s.authMiddleware)
		}

		r.Get("/accounts", s.listAccounts)
		r.Get("/accounts/{id}", s.getAccount)
		r.Post("/accounts", s.createAccount)
		r.Put("/accounts/{id}", s.updateAccount)
		r.Delete("/accounts/{id}", s.deleteAccount)
		r.Post("/accounts/{id}/reset-breaker", s.resetBreaker)

		r.Get("/groups", s.listGroups)
		r.Get("/groups/{id}", s.getGroup)
		r.Post("/groups", s.createGroup)
		r.Put("/groups/{id}", s.updateGroup)
		r.Delete("/groups/{id}", s.deleteGroup)

		r.Get("/keys", s.listKeys)
		r.Get("/keys/{id}", s.getKey)
		r.Post("/keys", s.createKey)
		r.Put("/keys/{id}", s.updateKey)
		r.Delete("/keys/{id}", s.deleteKey)

		r.Put("/accounts/{id}/status", s.updateAccountStatus)
		r.Put("/keys/{id}/status", s.updateKeyStatus)
		r.Post("/keys/{id}/rotate", s.rotateKey)

		r.Get("/logs", s.queryLogs)
		r.Get("/logs/{id}/payload", s.getPayload)
		r.Post("/cleanup", s.cleanup)
		r.Get("/stats/cost", s.costStats)
		r.Get("/health", s.health)
	})

	r.NotFound(frontendHandler())

	return r
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if token == "" || token != "Bearer "+s.adminToken {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) listAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := s.accountRepo.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	result := make([]map[string]any, 0, len(accounts))
	for _, account := range accounts {
		info := sanitizeAccount(&account)
		info["breaker_state"] = s.router.GetBreakerState(account.ID).String()
		result = append(result, info)
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) getAccount(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	account, err := s.accountRepo.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if account == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "account not found"})
		return
	}

	info := sanitizeAccount(account)
	info["breaker_state"] = s.router.GetBreakerState(account.ID).String()
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) createAccount(w http.ResponseWriter, r *http.Request) {
	var account domain.Account
	if err := json.NewDecoder(r.Body).Decode(&account); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if account.ID == "" {
		account.ID = generateID("acc-")
	}
	if account.Status == "" {
		account.Status = domain.AccountEnabled
	}
	if err := validateAccount(account); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if err := s.accountRepo.Create(r.Context(), &account); err != nil {
		writeJSON(w, statusFromWriteError(err), map[string]string{"error": err.Error()})
		return
	}
	if err := s.reloadFn(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, sanitizeAccount(&account))
}

func (s *Server) updateAccount(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	current, err := s.accountRepo.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if current == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "account not found"})
		return
	}

	var updates map[string]any
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if err := mergeAccountUpdate(current, updates); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	current.ID = id
	if err := validateAccount(*current); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if err := s.accountRepo.Update(r.Context(), current); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := s.reloadFn(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, sanitizeAccount(current))
}

func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	account, err := s.accountRepo.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if account == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "account not found"})
		return
	}
	groups, err := s.groupRepo.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	for _, group := range groups {
		for _, accountID := range group.AccountIDs {
			if accountID == id {
				writeJSON(w, http.StatusConflict, map[string]string{
					"error": fmt.Sprintf("cannot delete account: referenced by group %q", group.ID),
				})
				return
			}
		}
	}

	if err := s.accountRepo.Delete(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := s.reloadFn(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) resetBreaker(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	account, err := s.accountRepo.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if account == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "account not found"})
		return
	}

	s.router.ResetBreaker(id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "breaker reset"})
}

func (s *Server) listGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := s.groupRepo.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if groups == nil {
		groups = []domain.KeyGroup{}
	}
	writeJSON(w, http.StatusOK, groups)
}

func (s *Server) getGroup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	group, err := s.groupRepo.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if group == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "group not found"})
		return
	}
	writeJSON(w, http.StatusOK, group)
}

func (s *Server) createGroup(w http.ResponseWriter, r *http.Request) {
	var group domain.KeyGroup
	if err := json.NewDecoder(r.Body).Decode(&group); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if group.ID == "" {
		group.ID = generateID("grp-")
	}
	if err := s.validateGroup(r.Context(), group); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if err := s.groupRepo.Create(r.Context(), &group); err != nil {
		writeJSON(w, statusFromWriteError(err), map[string]string{"error": err.Error()})
		return
	}
	if err := s.reloadFn(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, group)
}

func (s *Server) updateGroup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	current, err := s.groupRepo.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if current == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "group not found"})
		return
	}

	var updates map[string]any
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if err := mergeGroupUpdate(current, updates); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	current.ID = id
	if err := s.validateGroup(r.Context(), *current); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if err := s.groupRepo.Update(r.Context(), current); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := s.reloadFn(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, current)
}

func (s *Server) deleteGroup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	group, err := s.groupRepo.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if group == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "group not found"})
		return
	}
	keys, err := s.keyRepo.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	for _, key := range keys {
		if key.GroupID == id {
			writeJSON(w, http.StatusConflict, map[string]string{
				"error": fmt.Sprintf("cannot delete group: referenced by api key %q", key.ID),
			})
			return
		}
	}

	if err := s.groupRepo.Delete(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := s.reloadFn(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) listKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.keyRepo.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if keys == nil {
		keys = []domain.ExternalAPIKey{}
	}
	if err := s.attachCurrentMonthUsage(r.Context(), keys); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, keys)
}

func (s *Server) getKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	key, err := s.keyRepo.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if key == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "key not found"})
		return
	}
	if err := s.attachCurrentMonthUsageToKey(r.Context(), key); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, key)
}

func (s *Server) createKey(w http.ResponseWriter, r *http.Request) {
	var key domain.ExternalAPIKey
	if err := json.NewDecoder(r.Body).Decode(&key); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if key.ID == "" {
		key.ID = generateID("key-")
	}
	if key.Status == "" {
		key.Status = domain.AccountEnabled
	}
	if err := s.validateKey(r.Context(), key); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	rawKey, err := generateAPIKey()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if err := s.keyRepo.Create(r.Context(), &key, rawKey); err != nil {
		writeJSON(w, statusFromWriteError(err), map[string]string{"error": err.Error()})
		return
	}
	if err := s.reloadFn(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	created, err := s.keyRepo.Get(r.Context(), key.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := s.attachCurrentMonthUsageToKey(r.Context(), created); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"key":     created,
		"raw_key": rawKey,
	})
}

func (s *Server) updateKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	current, err := s.keyRepo.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if current == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "key not found"})
		return
	}

	var updates map[string]any
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if err := mergeKeyUpdate(current, updates); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	current.ID = id
	if err := s.validateKey(r.Context(), *current); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if err := s.keyRepo.Update(r.Context(), current); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := s.reloadFn(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	updated, err := s.keyRepo.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := s.attachCurrentMonthUsageToKey(r.Context(), updated); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) updateAccountStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	account, err := s.accountRepo.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if account == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "account not found"})
		return
	}

	status, err := decodeStatusBody(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	account.Status = status

	if err := s.accountRepo.Update(r.Context(), account); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := s.reloadFn(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, sanitizeAccount(account))
}

func (s *Server) updateKeyStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	key, err := s.keyRepo.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if key == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "key not found"})
		return
	}

	status, err := decodeStatusBody(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	key.Status = status

	if err := s.keyRepo.Update(r.Context(), key); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := s.reloadFn(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	updated, err := s.keyRepo.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := s.attachCurrentMonthUsageToKey(r.Context(), updated); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) rotateKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	key, err := s.keyRepo.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if key == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "key not found"})
		return
	}

	rawKey, err := generateAPIKey()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if err := s.keyRepo.Rotate(r.Context(), id, rawKey); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := s.reloadFn(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	rotated, err := s.keyRepo.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := s.attachCurrentMonthUsageToKey(r.Context(), rotated); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"key":     rotated,
		"raw_key": rawKey,
	})
}

func (s *Server) deleteKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	key, err := s.keyRepo.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if key == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "key not found"})
		return
	}

	if err := s.keyRepo.Delete(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := s.reloadFn(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) queryLogs(w http.ResponseWriter, r *http.Request) {
	filter := store.LogFilter{
		KeyID:     r.URL.Query().Get("key_id"),
		AccountID: r.URL.Query().Get("account_id"),
		Model:     r.URL.Query().Get("model"),
	}

	if from := r.URL.Query().Get("from"); from != "" {
		if t, err := time.Parse(time.RFC3339, from); err == nil {
			filter.From = t
		}
	}
	if to := r.URL.Query().Get("to"); to != "" {
		if t, err := time.Parse(time.RFC3339, to); err == nil {
			filter.To = t
		}
	}
	if limit := r.URL.Query().Get("limit"); limit != "" {
		if n, err := strconv.Atoi(limit); err == nil {
			filter.Limit = n
		}
	}
	if offset := r.URL.Query().Get("offset"); offset != "" {
		if n, err := strconv.Atoi(offset); err == nil {
			filter.Offset = n
		}
	}

	logs, err := s.logRepo.QueryLogs(r.Context(), filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if logs == nil {
		logs = []store.RequestLog{}
	}
	writeJSON(w, http.StatusOK, logs)
}

func (s *Server) getPayload(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid log id"})
		return
	}

	payload, err := s.logRepo.GetPayload(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if payload == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "payload not found or expired"})
		return
	}

	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) costStats(w http.ResponseWriter, r *http.Request) {
	groupBy := r.URL.Query().Get("group_by")
	if groupBy == "" {
		groupBy = "account"
	}

	var from time.Time
	if raw := r.URL.Query().Get("from"); raw != "" {
		from, _ = time.Parse(time.RFC3339, raw)
	}

	var to time.Time
	if raw := r.URL.Query().Get("to"); raw != "" {
		to, _ = time.Parse(time.RFC3339, raw)
	}

	stats, err := s.logRepo.QueryCostStats(r.Context(), groupBy, from, to)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) cleanup(w http.ResponseWriter, _ *http.Request) {
	if s.cleaner == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "cleanup is not configured"})
		return
	}

	deleted, err := s.cleaner.CleanNow()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"deleted": deleted,
	})
}

func (s *Server) reload() error {
	if s.reloadFn == nil {
		return nil
	}
	return s.reloadFn()
}

func generateAPIKey() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "sk-" + hex.EncodeToString(buf), nil
}

func statusFromWriteError(err error) int {
	if errors.Is(err, context.Canceled) {
		return http.StatusRequestTimeout
	}
	return http.StatusInternalServerError
}

func (s *Server) attachCurrentMonthUsage(ctx context.Context, keys []domain.ExternalAPIKey) error {
	if len(keys) == 0 {
		return nil
	}

	usage, err := s.keyRepo.GetAllUsage(ctx, time.Now().Format("2006-01"))
	if err != nil {
		return err
	}

	for i := range keys {
		if item, ok := usage[keys[i].ID]; ok {
			keys[i].UsedInputTokens = item[0]
			keys[i].UsedOutputTokens = item[1]
		} else {
			keys[i].UsedInputTokens = 0
			keys[i].UsedOutputTokens = 0
		}
	}

	return nil
}

func (s *Server) attachCurrentMonthUsageToKey(ctx context.Context, key *domain.ExternalAPIKey) error {
	if key == nil {
		return nil
	}

	inputTokens, outputTokens, err := s.keyRepo.GetUsage(ctx, key.ID, time.Now().Format("2006-01"))
	if err != nil {
		return err
	}

	key.UsedInputTokens = inputTokens
	key.UsedOutputTokens = outputTokens
	return nil
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func maskKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
}

func sanitizeAccount(a *domain.Account) map[string]any {
	return map[string]any{
		"id":              a.ID,
		"name":            a.Name,
		"provider":        a.Provider,
		"api_key":         maskKey(a.APIKey),
		"base_url":        a.BaseURL,
		"proxy_url":       a.ProxyURL,
		"user_agent":      a.UserAgent,
		"status":          a.Status,
		"allowed_models":  a.AllowedModels,
		"model_aliases":   a.ModelAliases,
		"max_concurrent":  a.MaxConcurrent,
		"circuit_breaker": a.CircuitBreaker,
		"extra":           a.Extra,
	}
}

func generateID(prefix string) string {
	buf := make([]byte, 6)
	_, _ = rand.Read(buf)
	return prefix + hex.EncodeToString(buf)[:6]
}

func validateAccount(account domain.Account) error {
	if account.Provider == "" {
		return errors.New("provider is required")
	}
	if !isValidProvider(account.Provider) {
		return fmt.Errorf("invalid provider %q", account.Provider)
	}
	if account.APIKey == "" {
		return errors.New("api_key is required")
	}
	if !isValidStatus(account.Status) {
		return fmt.Errorf("invalid status %q", account.Status)
	}
	return nil
}

func (s *Server) validateGroup(ctx context.Context, group domain.KeyGroup) error {
	if len(group.AccountIDs) == 0 {
		return errors.New("account_ids must not be empty")
	}
	if !isValidBalancer(group.Balancer) {
		return fmt.Errorf("invalid balancer %q", group.Balancer)
	}
	for _, accountID := range group.AccountIDs {
		account, err := s.accountRepo.Get(ctx, accountID)
		if err != nil {
			return err
		}
		if account == nil {
			return fmt.Errorf("account %q does not exist", accountID)
		}
	}
	return nil
}

func (s *Server) validateKey(ctx context.Context, key domain.ExternalAPIKey) error {
	if key.GroupID == "" {
		return errors.New("group_id is required")
	}
	group, err := s.groupRepo.Get(ctx, key.GroupID)
	if err != nil {
		return err
	}
	if group == nil {
		return fmt.Errorf("group %q does not exist", key.GroupID)
	}
	if !isValidStatus(key.Status) {
		return fmt.Errorf("invalid status %q", key.Status)
	}
	return nil
}

func mergeAccountUpdate(current *domain.Account, updates map[string]any) error {
	for field, value := range updates {
		switch field {
		case "id", "health":
			continue
		case "name":
			v, err := asString(value, field)
			if err != nil {
				return err
			}
			current.Name = v
		case "provider":
			v, err := asString(value, field)
			if err != nil {
				return err
			}
			current.Provider = domain.ProviderType(v)
		case "base_url":
			v, err := asString(value, field)
			if err != nil {
				return err
			}
			current.BaseURL = v
		case "api_key":
			v, err := asString(value, field)
			if err != nil {
				return err
			}
			current.APIKey = v
		case "proxy_url":
			v, err := asString(value, field)
			if err != nil {
				return err
			}
			current.ProxyURL = v
		case "user_agent":
			v, err := asString(value, field)
			if err != nil {
				return err
			}
			current.UserAgent = v
		case "status":
			v, err := asString(value, field)
			if err != nil {
				return err
			}
			current.Status = domain.AccountStatus(v)
		case "allowed_models":
			v, err := asStringSlice(value, field)
			if err != nil {
				return err
			}
			current.AllowedModels = v
		case "model_aliases":
			v, err := asStringMap(value, field)
			if err != nil {
				return err
			}
			current.ModelAliases = v
		case "max_concurrent":
			v, err := asInt(value, field)
			if err != nil {
				return err
			}
			current.MaxConcurrent = v
		case "extra":
			v, err := asAnyMap(value, field)
			if err != nil {
				return err
			}
			current.Extra = v
		case "circuit_breaker":
			v, err := asCircuitBreakerConfig(current.CircuitBreaker, value, field)
			if err != nil {
				return err
			}
			current.CircuitBreaker = v
		default:
			return fmt.Errorf("unknown field %q", field)
		}
	}
	return nil
}

func mergeGroupUpdate(current *domain.KeyGroup, updates map[string]any) error {
	for field, value := range updates {
		switch field {
		case "id":
			continue
		case "name":
			v, err := asString(value, field)
			if err != nil {
				return err
			}
			current.Name = v
		case "account_ids":
			v, err := asStringSlice(value, field)
			if err != nil {
				return err
			}
			current.AccountIDs = v
		case "allowed_models":
			v, err := asStringSlice(value, field)
			if err != nil {
				return err
			}
			current.AllowedModels = v
		case "balancer":
			v, err := asString(value, field)
			if err != nil {
				return err
			}
			current.Balancer = v
		default:
			return fmt.Errorf("unknown field %q", field)
		}
	}
	return nil
}

func mergeKeyUpdate(current *domain.ExternalAPIKey, updates map[string]any) error {
	for field, value := range updates {
		switch field {
		case "id", "key", "key_hint", "created_at", "used_input_tokens", "used_output_tokens":
			continue
		case "group_id":
			v, err := asString(value, field)
			if err != nil {
				return err
			}
			current.GroupID = v
		case "status":
			v, err := asString(value, field)
			if err != nil {
				return err
			}
			current.Status = domain.AccountStatus(v)
		case "allowed_models":
			v, err := asStringSlice(value, field)
			if err != nil {
				return err
			}
			current.AllowedModels = v
		case "max_input_tokens_monthly":
			v, err := asInt64(value, field)
			if err != nil {
				return err
			}
			current.MaxInputTokens = v
		case "max_output_tokens_monthly":
			v, err := asInt64(value, field)
			if err != nil {
				return err
			}
			current.MaxOutputTokens = v
		case "max_concurrent":
			v, err := asInt(value, field)
			if err != nil {
				return err
			}
			current.MaxConcurrent = v
		default:
			return fmt.Errorf("unknown field %q", field)
		}
	}
	return nil
}

func decodeStatusBody(r *http.Request) (domain.AccountStatus, error) {
	var payload struct {
		Status domain.AccountStatus `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		return "", errors.New("invalid request body")
	}
	if !isValidStatus(payload.Status) {
		return "", fmt.Errorf("invalid status %q", payload.Status)
	}
	return payload.Status, nil
}

func isValidProvider(provider domain.ProviderType) bool {
	switch provider {
	case domain.ProviderAnthropic, domain.ProviderOpenAI, domain.ProviderGemini, domain.ProviderCustomOpenAI, domain.ProviderCustomAnthropic:
		return true
	default:
		return false
	}
}

func isValidStatus(status domain.AccountStatus) bool {
	switch status {
	case domain.AccountEnabled, domain.AccountDisabled:
		return true
	default:
		return false
	}
}

func isValidBalancer(name string) bool {
	switch name {
	case "", "round_robin", "least_connections", "weighted", "priority":
		return true
	default:
		return false
	}
}

func asString(value any, field string) (string, error) {
	if value == nil {
		return "", nil
	}
	v, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", field)
	}
	return v, nil
}

func asStringSlice(value any, field string) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an array of strings", field)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("%s must be an array of strings", field)
		}
		out = append(out, s)
	}
	return out, nil
}

func asStringMap(value any, field string) (map[string]string, error) {
	if value == nil {
		return nil, nil
	}
	items, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an object", field)
	}
	out := make(map[string]string, len(items))
	for k, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("%s must contain only string values", field)
		}
		out[k] = s
	}
	return out, nil
}

func asAnyMap(value any, field string) (map[string]any, error) {
	if value == nil {
		return nil, nil
	}
	items, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an object", field)
	}
	return items, nil
}

func asInt(value any, field string) (int, error) {
	if value == nil {
		return 0, nil
	}
	n, ok := value.(float64)
	if !ok {
		return 0, fmt.Errorf("%s must be a number", field)
	}
	if float64(int(n)) != n {
		return 0, fmt.Errorf("%s must be an integer", field)
	}
	return int(n), nil
}

func asInt64(value any, field string) (int64, error) {
	if value == nil {
		return 0, nil
	}
	n, ok := value.(float64)
	if !ok {
		return 0, fmt.Errorf("%s must be a number", field)
	}
	if float64(int64(n)) != n {
		return 0, fmt.Errorf("%s must be an integer", field)
	}
	return int64(n), nil
}

func asCircuitBreakerConfig(current domain.CircuitBreakerConfig, value any, field string) (domain.CircuitBreakerConfig, error) {
	if value == nil {
		return domain.CircuitBreakerConfig{}, nil
	}
	items, ok := value.(map[string]any)
	if !ok {
		return domain.CircuitBreakerConfig{}, fmt.Errorf("%s must be an object", field)
	}

	cfg := current
	for key, item := range items {
		switch key {
		case "failure_threshold":
			v, err := asInt(item, key)
			if err != nil {
				return domain.CircuitBreakerConfig{}, err
			}
			cfg.FailureThreshold = v
		case "success_threshold":
			v, err := asInt(item, key)
			if err != nil {
				return domain.CircuitBreakerConfig{}, err
			}
			cfg.SuccessThreshold = v
		case "open_duration":
			v, err := asString(item, key)
			if err != nil {
				return domain.CircuitBreakerConfig{}, err
			}
			cfg.OpenDuration = v
		default:
			return domain.CircuitBreakerConfig{}, fmt.Errorf("unknown field %q in %s", key, field)
		}
	}
	return cfg, nil
}
