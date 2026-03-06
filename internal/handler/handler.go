package handler

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/ViniZap4/devnook-server/internal/auth"
	"github.com/ViniZap4/devnook-server/internal/config"
	"github.com/ViniZap4/devnook-server/internal/domain"
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

// userColumns is the standard column list for scanning a User (without password).
const userColumns = `id, username, email, full_name, avatar_url, bio, location, website, is_admin, created_at, updated_at`

// userColumnsAs returns the column list with a table alias prefix (e.g. "u.").
const userColumnsAs = `u.id, u.username, u.email, u.full_name, u.avatar_url, u.bio, u.location, u.website, u.is_admin, u.created_at, u.updated_at`

// scanUser returns pointers to all scannable User fields in column order.
func scanUser(u *domain.User) []any {
	return []any{&u.ID, &u.Username, &u.Email, &u.FullName, &u.AvatarURL,
		&u.Bio, &u.Location, &u.Website, &u.IsAdmin, &u.CreatedAt, &u.UpdatedAt}
}

// resolveUserID looks up a user by username and returns their ID.
func (h *Handler) resolveUserID(ctx context.Context, username string) (int64, error) {
	var id int64
	err := h.db.QueryRow(ctx, `SELECT id FROM users WHERE username = $1`, username).Scan(&id)
	return id, err
}

// scanUsers scans rows of user columns into a User slice, returning an empty (non-nil) slice if there are no rows.
func scanUsers(rows interface {
	Next() bool
	Scan(dest ...any) error
}) []domain.User {
	var users []domain.User
	for rows.Next() {
		var u domain.User
		if err := rows.Scan(scanUser(&u)...); err != nil {
			continue
		}
		users = append(users, u)
	}
	if users == nil {
		return []domain.User{}
	}
	return users
}
