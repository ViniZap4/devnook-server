package handler

import (
	"context"
	"strings"

	"github.com/ViniZap4/devnook-server/internal/auth"
	"github.com/ViniZap4/devnook-server/internal/config"
	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/ViniZap4/devnook-server/internal/ws"
	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

const userClaimsKey = "claims"

type Handler struct {
	db  *pgxpool.Pool
	cfg *config.Config
	hub *ws.Hub
}

func New(db *pgxpool.Pool, cfg *config.Config, hub *ws.Hub) *Handler {
	return &Handler{db: db, cfg: cfg, hub: hub}
}

func (h *Handler) Health(c *fiber.Ctx) error {
	return writeJSON(c, fiber.StatusOK, fiber.Map{"status": "ok"})
}

func (h *Handler) AuthMiddleware(c *fiber.Ctx) error {
	header := c.Get("Authorization")
	if header == "" {
		return writeError(c, fiber.StatusUnauthorized, "missing authorization header")
	}
	tokenStr := strings.TrimPrefix(header, "Bearer ")
	claims, err := auth.ValidateToken(tokenStr, h.cfg.Secret)
	if err != nil {
		return writeError(c, fiber.StatusUnauthorized, "invalid token")
	}
	c.Locals(userClaimsKey, claims)
	return c.Next()
}

func getClaims(c *fiber.Ctx) *auth.Claims {
	claims, _ := c.Locals(userClaimsKey).(*auth.Claims)
	return claims
}

func writeJSON(c *fiber.Ctx, status int, data any) error {
	return c.Status(status).JSON(data)
}

func writeError(c *fiber.Ctx, status int, msg string) error {
	return c.Status(status).JSON(fiber.Map{"error": msg})
}

func readJSON(c *fiber.Ctx, v any) error {
	return c.BodyParser(v)
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
