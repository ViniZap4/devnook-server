package handler

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/ViniZap4/devnook-server/internal/auth"
	"github.com/ViniZap4/devnook-server/internal/config"
	"github.com/ViniZap4/devnook-server/internal/ws"
	"github.com/jackc/pgx/v5/pgxpool"
)

type contextKey string

const userClaimsKey contextKey = "claims"

type Handler struct {
	db  *pgxpool.Pool
	cfg *config.Config
	hub *ws.Hub
}

func New(db *pgxpool.Pool, cfg *config.Config, hub *ws.Hub) *Handler {
	return &Handler{db: db, cfg: cfg, hub: hub}
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if header == "" {
			writeError(w, http.StatusUnauthorized, "missing authorization header")
			return
		}
		tokenStr := strings.TrimPrefix(header, "Bearer ")
		claims, err := auth.ValidateToken(tokenStr, h.cfg.Secret)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		ctx := context.WithValue(r.Context(), userClaimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func getClaims(r *http.Request) *auth.Claims {
	return r.Context().Value(userClaimsKey).(*auth.Claims)
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("writeJSON encode error: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}
