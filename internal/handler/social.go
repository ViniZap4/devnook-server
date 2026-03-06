package handler

import (
	"context"
	"net/http"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/go-chi/chi/v5"
)

// ── Search ──────────────────────────────────────────────────────────

func (h *Handler) SearchUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeJSON(w, http.StatusOK, []domain.User{})
		return
	}

	pattern := "%" + q + "%"
	rows, err := h.db.Query(context.Background(),
		`SELECT `+userColumns+` FROM users WHERE username ILIKE $1 OR full_name ILIKE $1 ORDER BY username LIMIT 20`,
		pattern)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search failed")
		return
	}
	defer rows.Close()

	writeJSON(w, http.StatusOK, scanUsers(rows))
}

// ── Follow ──────────────────────────────────────────────────────────

func (h *Handler) FollowUser(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	ctx := context.Background()

	targetID, err := h.resolveUserID(ctx, chi.URLParam(r, "username"))
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if targetID == claims.UserID {
		writeError(w, http.StatusBadRequest, "cannot follow yourself")
		return
	}

	_, err = h.db.Exec(ctx,
		`INSERT INTO follows (follower_id, following_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		claims.UserID, targetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to follow")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) UnfollowUser(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	ctx := context.Background()

	targetID, err := h.resolveUserID(ctx, chi.URLParam(r, "username"))
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	_, err = h.db.Exec(ctx,
		`DELETE FROM follows WHERE follower_id = $1 AND following_id = $2`,
		claims.UserID, targetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unfollow")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) IsFollowing(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	ctx := context.Background()

	targetID, err := h.resolveUserID(ctx, chi.URLParam(r, "username"))
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	var count int
	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM follows WHERE follower_id = $1 AND following_id = $2`,
		claims.UserID, targetID).Scan(&count)

	writeJSON(w, http.StatusOK, map[string]bool{"following": count > 0})
}

func (h *Handler) GetFollowers(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")

	rows, err := h.db.Query(context.Background(),
		`SELECT `+userColumnsAs+` FROM users u
		 JOIN follows f ON f.follower_id = u.id
		 JOIN users t ON t.id = f.following_id
		 WHERE t.username = $1
		 ORDER BY f.created_at DESC`, username)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get followers")
		return
	}
	defer rows.Close()

	writeJSON(w, http.StatusOK, scanUsers(rows))
}

func (h *Handler) GetFollowing(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")

	rows, err := h.db.Query(context.Background(),
		`SELECT `+userColumnsAs+` FROM users u
		 JOIN follows f ON f.following_id = u.id
		 JOIN users t ON t.id = f.follower_id
		 WHERE t.username = $1
		 ORDER BY f.created_at DESC`, username)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get following")
		return
	}
	defer rows.Close()

	writeJSON(w, http.StatusOK, scanUsers(rows))
}

// ── Block ───────────────────────────────────────────────────────────

func (h *Handler) BlockUser(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	ctx := context.Background()

	targetID, err := h.resolveUserID(ctx, chi.URLParam(r, "username"))
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if targetID == claims.UserID {
		writeError(w, http.StatusBadRequest, "cannot block yourself")
		return
	}

	// Remove any follow relationships in both directions
	h.db.Exec(ctx, `DELETE FROM follows WHERE (follower_id=$1 AND following_id=$2) OR (follower_id=$2 AND following_id=$1)`,
		claims.UserID, targetID)

	_, err = h.db.Exec(ctx,
		`INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		claims.UserID, targetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to block")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) UnblockUser(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	ctx := context.Background()

	targetID, err := h.resolveUserID(ctx, chi.URLParam(r, "username"))
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	_, err = h.db.Exec(ctx,
		`DELETE FROM blocks WHERE blocker_id = $1 AND blocked_id = $2`,
		claims.UserID, targetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unblock")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) IsBlocked(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	ctx := context.Background()

	targetID, err := h.resolveUserID(ctx, chi.URLParam(r, "username"))
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	var count int
	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM blocks WHERE blocker_id = $1 AND blocked_id = $2`,
		claims.UserID, targetID).Scan(&count)

	writeJSON(w, http.StatusOK, map[string]bool{"blocked": count > 0})
}

func (h *Handler) ListBlockedUsers(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)

	rows, err := h.db.Query(context.Background(),
		`SELECT `+userColumnsAs+` FROM users u
		 JOIN blocks b ON b.blocked_id = u.id
		 WHERE b.blocker_id = $1
		 ORDER BY b.created_at DESC`, claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list blocked users")
		return
	}
	defer rows.Close()

	writeJSON(w, http.StatusOK, scanUsers(rows))
}

// ── Status ──────────────────────────────────────────────────────────

func (h *Handler) SetStatus(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	var req struct {
		Emoji   string `json:"emoji"`
		Message string `json:"message"`
		Busy    bool   `json:"busy"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	_, err := h.db.Exec(context.Background(),
		`INSERT INTO user_status (user_id, emoji, message, busy, updated_at)
		 VALUES ($1, $2, $3, $4, NOW())
		 ON CONFLICT (user_id) DO UPDATE SET emoji=$2, message=$3, busy=$4, updated_at=NOW()`,
		claims.UserID, req.Emoji, req.Message, req.Busy)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to set status")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) GetStatus(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")

	var status domain.UserStatus
	err := h.db.QueryRow(context.Background(),
		`SELECT s.emoji, s.message, s.busy
		 FROM user_status s
		 JOIN users u ON u.id = s.user_id
		 WHERE u.username = $1`, username,
	).Scan(&status.Emoji, &status.Message, &status.Busy)
	if err != nil {
		writeJSON(w, http.StatusOK, domain.UserStatus{})
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *Handler) ClearStatus(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	h.db.Exec(context.Background(),
		`DELETE FROM user_status WHERE user_id = $1`, claims.UserID)
	w.WriteHeader(http.StatusNoContent)
}

