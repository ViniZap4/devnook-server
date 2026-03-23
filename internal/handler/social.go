package handler

import (
	"context"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/gofiber/fiber/v2"
)

// ── Search ──────────────────────────────────────────────────────────

func (h *Handler) SearchUsers(c *fiber.Ctx) error {
	q := c.Query("q")
	if q == "" {
		return writeJSON(c, fiber.StatusOK, []domain.User{})
	}

	pattern := "%" + q + "%"
	rows, err := h.db.Query(context.Background(),
		`SELECT `+userColumns+` FROM users WHERE username ILIKE $1 OR full_name ILIKE $1 ORDER BY username LIMIT 20`,
		pattern)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "search failed")
	}
	defer rows.Close()

	return writeJSON(c, fiber.StatusOK, scanUsers(rows))
}

// ── Follow ──────────────────────────────────────────────────────────

func (h *Handler) FollowUser(c *fiber.Ctx) error {
	claims := getClaims(c)
	ctx := context.Background()

	targetID, err := h.resolveUserID(ctx, c.Params("username"))
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "user not found")
	}
	if targetID == claims.UserID {
		return writeError(c, fiber.StatusBadRequest, "cannot follow yourself")
	}

	_, err = h.db.Exec(ctx,
		`INSERT INTO follows (follower_id, following_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		claims.UserID, targetID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to follow")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) UnfollowUser(c *fiber.Ctx) error {
	claims := getClaims(c)
	ctx := context.Background()

	targetID, err := h.resolveUserID(ctx, c.Params("username"))
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "user not found")
	}

	_, err = h.db.Exec(ctx,
		`DELETE FROM follows WHERE follower_id = $1 AND following_id = $2`,
		claims.UserID, targetID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to unfollow")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) IsFollowing(c *fiber.Ctx) error {
	claims := getClaims(c)
	ctx := context.Background()

	targetID, err := h.resolveUserID(ctx, c.Params("username"))
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "user not found")
	}

	var count int
	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM follows WHERE follower_id = $1 AND following_id = $2`,
		claims.UserID, targetID).Scan(&count)

	return writeJSON(c, fiber.StatusOK, map[string]bool{"following": count > 0})
}

func (h *Handler) GetFollowers(c *fiber.Ctx) error {
	username := c.Params("username")

	rows, err := h.db.Query(context.Background(),
		`SELECT `+userColumnsAs+` FROM users u
		 JOIN follows f ON f.follower_id = u.id
		 JOIN users t ON t.id = f.following_id
		 WHERE t.username = $1
		 ORDER BY f.created_at DESC`, username)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to get followers")
	}
	defer rows.Close()

	return writeJSON(c, fiber.StatusOK, scanUsers(rows))
}

func (h *Handler) GetFollowing(c *fiber.Ctx) error {
	username := c.Params("username")

	rows, err := h.db.Query(context.Background(),
		`SELECT `+userColumnsAs+` FROM users u
		 JOIN follows f ON f.following_id = u.id
		 JOIN users t ON t.id = f.follower_id
		 WHERE t.username = $1
		 ORDER BY f.created_at DESC`, username)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to get following")
	}
	defer rows.Close()

	return writeJSON(c, fiber.StatusOK, scanUsers(rows))
}

// ── Block ───────────────────────────────────────────────────────────

func (h *Handler) BlockUser(c *fiber.Ctx) error {
	claims := getClaims(c)
	ctx := context.Background()

	targetID, err := h.resolveUserID(ctx, c.Params("username"))
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "user not found")
	}
	if targetID == claims.UserID {
		return writeError(c, fiber.StatusBadRequest, "cannot block yourself")
	}

	// Remove any follow relationships in both directions
	h.db.Exec(ctx, `DELETE FROM follows WHERE (follower_id=$1 AND following_id=$2) OR (follower_id=$2 AND following_id=$1)`,
		claims.UserID, targetID)

	_, err = h.db.Exec(ctx,
		`INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		claims.UserID, targetID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to block")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) UnblockUser(c *fiber.Ctx) error {
	claims := getClaims(c)
	ctx := context.Background()

	targetID, err := h.resolveUserID(ctx, c.Params("username"))
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "user not found")
	}

	_, err = h.db.Exec(ctx,
		`DELETE FROM blocks WHERE blocker_id = $1 AND blocked_id = $2`,
		claims.UserID, targetID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to unblock")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) IsBlocked(c *fiber.Ctx) error {
	claims := getClaims(c)
	ctx := context.Background()

	targetID, err := h.resolveUserID(ctx, c.Params("username"))
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "user not found")
	}

	var count int
	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM blocks WHERE blocker_id = $1 AND blocked_id = $2`,
		claims.UserID, targetID).Scan(&count)

	return writeJSON(c, fiber.StatusOK, map[string]bool{"blocked": count > 0})
}

func (h *Handler) ListBlockedUsers(c *fiber.Ctx) error {
	claims := getClaims(c)

	rows, err := h.db.Query(context.Background(),
		`SELECT `+userColumnsAs+` FROM users u
		 JOIN blocks b ON b.blocked_id = u.id
		 WHERE b.blocker_id = $1
		 ORDER BY b.created_at DESC`, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to list blocked users")
	}
	defer rows.Close()

	return writeJSON(c, fiber.StatusOK, scanUsers(rows))
}

// ── Status ──────────────────────────────────────────────────────────

func (h *Handler) SetStatus(c *fiber.Ctx) error {
	claims := getClaims(c)
	var req struct {
		Emoji   string `json:"emoji"`
		Message string `json:"message"`
		Busy    bool   `json:"busy"`
	}
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}

	_, err := h.db.Exec(context.Background(),
		`INSERT INTO user_status (user_id, emoji, message, busy, updated_at)
		 VALUES ($1, $2, $3, $4, NOW())
		 ON CONFLICT (user_id) DO UPDATE SET emoji=$2, message=$3, busy=$4, updated_at=NOW()`,
		claims.UserID, req.Emoji, req.Message, req.Busy)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to set status")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) GetStatus(c *fiber.Ctx) error {
	username := c.Params("username")

	var status domain.UserStatus
	err := h.db.QueryRow(context.Background(),
		`SELECT s.emoji, s.message, s.busy
		 FROM user_status s
		 JOIN users u ON u.id = s.user_id
		 WHERE u.username = $1`, username,
	).Scan(&status.Emoji, &status.Message, &status.Busy)
	if err != nil {
		return writeJSON(c, fiber.StatusOK, domain.UserStatus{})
	}
	return writeJSON(c, fiber.StatusOK, status)
}

func (h *Handler) ClearStatus(c *fiber.Ctx) error {
	claims := getClaims(c)
	h.db.Exec(context.Background(),
		`DELETE FROM user_status WHERE user_id = $1`, claims.UserID)
	return c.SendStatus(fiber.StatusNoContent)
}
