package handler

import (
	"context"
	"strconv"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/gofiber/fiber/v2"
)

func (h *Handler) ListShortcuts(c *fiber.Ctx) error {
	claims := getClaims(c)
	rows, err := h.db.Query(context.Background(),
		`SELECT id, user_id, title, url, icon_url, color, created_at, updated_at
		 FROM shortcuts WHERE user_id = $1 ORDER BY created_at`, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to list shortcuts")
	}
	defer rows.Close()

	var shortcuts []domain.Shortcut
	for rows.Next() {
		var s domain.Shortcut
		if err := rows.Scan(&s.ID, &s.UserID, &s.Title, &s.URL, &s.IconURL, &s.Color, &s.CreatedAt, &s.UpdatedAt); err != nil {
			continue
		}
		shortcuts = append(shortcuts, s)
	}
	if shortcuts == nil {
		shortcuts = []domain.Shortcut{}
	}
	return writeJSON(c, fiber.StatusOK, shortcuts)
}

type shortcutRequest struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	IconURL string `json:"icon_url"`
	Color   string `json:"color"`
}

func (h *Handler) CreateShortcut(c *fiber.Ctx) error {
	claims := getClaims(c)
	var req shortcutRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Title == "" || req.URL == "" {
		return writeError(c, fiber.StatusBadRequest, "title and url are required")
	}

	var id int64
	err := h.db.QueryRow(context.Background(),
		`INSERT INTO shortcuts (user_id, title, url, icon_url, color)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		claims.UserID, req.Title, req.URL, req.IconURL, req.Color,
	).Scan(&id)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to create shortcut")
	}

	return writeJSON(c, fiber.StatusCreated, map[string]interface{}{"id": id})
}

func (h *Handler) UpdateShortcut(c *fiber.Ctx) error {
	claims := getClaims(c)
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid shortcut id")
	}

	var req shortcutRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}

	tag, err := h.db.Exec(context.Background(),
		`UPDATE shortcuts SET title=$1, url=$2, icon_url=$3, color=$4, updated_at=NOW()
		 WHERE id=$5 AND user_id=$6`,
		req.Title, req.URL, req.IconURL, req.Color, id, claims.UserID)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "shortcut not found")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) DeleteShortcut(c *fiber.Ctx) error {
	claims := getClaims(c)
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid shortcut id")
	}

	tag, err := h.db.Exec(context.Background(),
		`DELETE FROM shortcuts WHERE id=$1 AND user_id=$2`, id, claims.UserID)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "shortcut not found")
	}
	return c.SendStatus(fiber.StatusNoContent)
}
