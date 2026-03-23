package handler

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/gofiber/fiber/v2"
)

type webhookRequest struct {
	URL    string   `json:"url"`
	Secret string   `json:"secret"`
	Events []string `json:"events"`
	Active *bool    `json:"active,omitempty"`
}

func (h *Handler) ListWebhooks(c *fiber.Ctx) error {
	owner := c.Params("owner")
	name := c.Params("name")

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "repository not found")
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT id, repo_id, url, secret, events, active, created_at, updated_at
		 FROM webhooks WHERE repo_id = $1 ORDER BY created_at DESC`, repoID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to list webhooks")
	}
	defer rows.Close()

	var hooks []domain.Webhook
	for rows.Next() {
		var wh domain.Webhook
		if err := rows.Scan(&wh.ID, &wh.RepoID, &wh.URL, &wh.Secret, &wh.Events,
			&wh.Active, &wh.CreatedAt, &wh.UpdatedAt); err != nil {
			continue
		}
		hooks = append(hooks, wh)
	}
	if hooks == nil {
		hooks = []domain.Webhook{}
	}
	return writeJSON(c, fiber.StatusOK, hooks)
}

func (h *Handler) CreateWebhook(c *fiber.Ctx) error {
	owner := c.Params("owner")
	name := c.Params("name")

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "repository not found")
	}

	var req webhookRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.URL == "" {
		return writeError(c, fiber.StatusBadRequest, "url is required")
	}
	if req.Events == nil {
		req.Events = []string{"push"}
	}

	var id int64
	err = h.db.QueryRow(context.Background(),
		`INSERT INTO webhooks (repo_id, url, secret, events) VALUES ($1, $2, $3, $4) RETURNING id`,
		repoID, req.URL, req.Secret, req.Events,
	).Scan(&id)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to create webhook")
	}
	return writeJSON(c, fiber.StatusCreated, map[string]any{"id": id})
}

func (h *Handler) UpdateWebhook(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid webhook id")
	}

	var req webhookRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}

	ctx := context.Background()
	sets := []string{}
	args := []any{}
	argN := 1

	if req.URL != "" {
		sets = append(sets, fmt.Sprintf("url=$%d", argN))
		args = append(args, req.URL)
		argN++
	}
	if req.Events != nil {
		sets = append(sets, fmt.Sprintf("events=$%d", argN))
		args = append(args, req.Events)
		argN++
	}
	if req.Active != nil {
		sets = append(sets, fmt.Sprintf("active=$%d", argN))
		args = append(args, *req.Active)
		argN++
	}
	if req.Secret != "" {
		sets = append(sets, fmt.Sprintf("secret=$%d", argN))
		args = append(args, req.Secret)
		argN++
	}

	if len(sets) == 0 {
		return c.SendStatus(fiber.StatusNoContent)
	}

	sets = append(sets, "updated_at=NOW()")
	query := fmt.Sprintf("UPDATE webhooks SET %s WHERE id=$%d",
		strings.Join(sets, ", "), argN)
	args = append(args, id)

	if _, err := h.db.Exec(ctx, query, args...); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to update webhook")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) DeleteWebhook(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid webhook id")
	}

	tag, err := h.db.Exec(context.Background(), `DELETE FROM webhooks WHERE id = $1`, id)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "webhook not found")
	}
	return c.SendStatus(fiber.StatusNoContent)
}
