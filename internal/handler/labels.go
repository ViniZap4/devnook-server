package handler

import (
	"context"
	"strconv"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/gofiber/fiber/v2"
)

type labelRequest struct {
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

func (h *Handler) ListLabels(c *fiber.Ctx) error {
	owner := c.Params("owner")
	name := c.Params("name")

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "repository not found")
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT id, repo_id, name, color, description FROM labels WHERE repo_id = $1 ORDER BY name`, repoID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to list labels")
	}
	defer rows.Close()

	var labels []domain.Label
	for rows.Next() {
		var l domain.Label
		if err := rows.Scan(&l.ID, &l.RepoID, &l.Name, &l.Color, &l.Description); err != nil {
			continue
		}
		labels = append(labels, l)
	}
	if labels == nil {
		labels = []domain.Label{}
	}
	return writeJSON(c, fiber.StatusOK, labels)
}

func (h *Handler) CreateLabel(c *fiber.Ctx) error {
	claims := getClaims(c)
	owner := c.Params("owner")
	name := c.Params("name")

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "repository not found")
	}

	// Verify caller owns the repo
	var ownerID int64
	err = h.db.QueryRow(context.Background(),
		`SELECT r.owner_id FROM repositories r WHERE r.id = $1`, repoID).Scan(&ownerID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "repository not found")
	}
	if claims.UserID != ownerID {
		return writeError(c, fiber.StatusForbidden, "only the repository owner can create labels")
	}

	var req labelRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Name == "" {
		return writeError(c, fiber.StatusBadRequest, "name is required")
	}
	if req.Color == "" {
		req.Color = "#cccccc"
	}

	var id int64
	err = h.db.QueryRow(context.Background(),
		`INSERT INTO labels (repo_id, name, color, description) VALUES ($1, $2, $3, $4) RETURNING id`,
		repoID, req.Name, req.Color, req.Description,
	).Scan(&id)
	if err != nil {
		return writeError(c, fiber.StatusConflict, "label already exists")
	}
	return writeJSON(c, fiber.StatusCreated, map[string]any{"id": id})
}

func (h *Handler) UpdateLabel(c *fiber.Ctx) error {
	claims := getClaims(c)
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid label id")
	}

	// Verify caller owns the repo this label belongs to
	var ownerID int64
	err = h.db.QueryRow(context.Background(),
		`SELECT r.owner_id FROM labels l JOIN repositories r ON r.id = l.repo_id WHERE l.id = $1`, id).Scan(&ownerID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "label not found")
	}
	if claims.UserID != ownerID {
		return writeError(c, fiber.StatusForbidden, "only the repository owner can update labels")
	}

	var req labelRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}

	tag, err := h.db.Exec(context.Background(),
		`UPDATE labels SET name = $1, color = $2, description = $3 WHERE id = $4`,
		req.Name, req.Color, req.Description, id)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "label not found")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) DeleteLabel(c *fiber.Ctx) error {
	claims := getClaims(c)
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid label id")
	}

	// Verify caller owns the repo this label belongs to
	var ownerID int64
	err = h.db.QueryRow(context.Background(),
		`SELECT r.owner_id FROM labels l JOIN repositories r ON r.id = l.repo_id WHERE l.id = $1`, id).Scan(&ownerID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "label not found")
	}
	if claims.UserID != ownerID {
		return writeError(c, fiber.StatusForbidden, "only the repository owner can delete labels")
	}

	h.db.Exec(context.Background(), `DELETE FROM labels WHERE id = $1`, id)
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) AddIssueLabel(c *fiber.Ctx) error {
	owner := c.Params("owner")
	name := c.Params("name")
	number, err := strconv.Atoi(c.Params("number"))
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid issue number")
	}

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "repository not found")
	}

	var req struct {
		LabelID int64 `json:"label_id"`
	}
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}

	var issueID int64
	err = h.db.QueryRow(context.Background(),
		`SELECT id FROM issues WHERE repo_id = $1 AND number = $2`, repoID, number,
	).Scan(&issueID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "issue not found")
	}

	_, err = h.db.Exec(context.Background(),
		`INSERT INTO issue_labels (issue_id, label_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		issueID, req.LabelID)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "failed to add label")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) RemoveIssueLabel(c *fiber.Ctx) error {
	owner := c.Params("owner")
	name := c.Params("name")
	number, err := strconv.Atoi(c.Params("number"))
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid issue number")
	}
	labelID, err := strconv.ParseInt(c.Params("labelId"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid label id")
	}

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "repository not found")
	}

	var issueID int64
	err = h.db.QueryRow(context.Background(),
		`SELECT id FROM issues WHERE repo_id = $1 AND number = $2`, repoID, number,
	).Scan(&issueID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "issue not found")
	}

	h.db.Exec(context.Background(),
		`DELETE FROM issue_labels WHERE issue_id = $1 AND label_id = $2`, issueID, labelID)
	return c.SendStatus(fiber.StatusNoContent)
}
