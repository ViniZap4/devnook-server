package handler

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/gofiber/fiber/v2"
)

type milestoneRequest struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	State       *string `json:"state,omitempty"`
	DueDate     *string `json:"due_date,omitempty"`
}

func (h *Handler) ListMilestones(c *fiber.Ctx) error {
	owner := c.Params("owner")
	name := c.Params("name")

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "repository not found")
	}

	state := c.Query("state")
	if state == "" {
		state = "open"
	}

	var query string
	var args []any
	if state == "all" {
		query = `SELECT m.id, m.repo_id, m.title, m.description, m.state, m.due_date, m.created_at, m.updated_at,
		                COALESCE(SUM(CASE WHEN i.state = 'open' THEN 1 ELSE 0 END), 0) AS open_issues,
		                COALESCE(SUM(CASE WHEN i.state = 'closed' THEN 1 ELSE 0 END), 0) AS closed_issues
		         FROM milestones m
		         LEFT JOIN issues i ON i.milestone_id = m.id
		         WHERE m.repo_id = $1
		         GROUP BY m.id
		         ORDER BY m.created_at DESC`
		args = []any{repoID}
	} else {
		query = `SELECT m.id, m.repo_id, m.title, m.description, m.state, m.due_date, m.created_at, m.updated_at,
		                COALESCE(SUM(CASE WHEN i.state = 'open' THEN 1 ELSE 0 END), 0) AS open_issues,
		                COALESCE(SUM(CASE WHEN i.state = 'closed' THEN 1 ELSE 0 END), 0) AS closed_issues
		         FROM milestones m
		         LEFT JOIN issues i ON i.milestone_id = m.id
		         WHERE m.repo_id = $1 AND m.state = $2
		         GROUP BY m.id
		         ORDER BY m.created_at DESC`
		args = []any{repoID, state}
	}

	rows, err := h.db.Query(context.Background(), query, args...)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to list milestones")
	}
	defer rows.Close()

	var milestones []domain.Milestone
	for rows.Next() {
		var m domain.Milestone
		if err := rows.Scan(&m.ID, &m.RepoID, &m.Title, &m.Description, &m.State, &m.DueDate, &m.CreatedAt, &m.UpdatedAt, &m.OpenIssues, &m.ClosedIssues); err != nil {
			continue
		}
		milestones = append(milestones, m)
	}
	if milestones == nil {
		milestones = []domain.Milestone{}
	}
	return writeJSON(c, fiber.StatusOK, milestones)
}

func (h *Handler) CreateMilestone(c *fiber.Ctx) error {
	owner := c.Params("owner")
	name := c.Params("name")

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "repository not found")
	}

	var req milestoneRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Title == "" {
		return writeError(c, fiber.StatusBadRequest, "title is required")
	}

	var dueDate *time.Time
	if req.DueDate != nil && *req.DueDate != "" {
		t, err := time.Parse(time.RFC3339, *req.DueDate)
		if err != nil {
			t, err = time.Parse("2006-01-02", *req.DueDate)
			if err != nil {
				return writeError(c, fiber.StatusBadRequest, "invalid due_date format")
			}
		}
		dueDate = &t
	}

	var id int64
	err = h.db.QueryRow(context.Background(),
		`INSERT INTO milestones (repo_id, title, description, due_date) VALUES ($1, $2, $3, $4) RETURNING id`,
		repoID, req.Title, req.Description, dueDate,
	).Scan(&id)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to create milestone")
	}
	return writeJSON(c, fiber.StatusCreated, map[string]any{"id": id})
}

func (h *Handler) UpdateMilestone(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid milestone id")
	}

	var req milestoneRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}

	ctx := context.Background()
	sets := []string{}
	args := []any{}
	argN := 1

	if req.Title != "" {
		sets = append(sets, fmt.Sprintf("title=$%d", argN))
		args = append(args, req.Title)
		argN++
	}
	if req.Description != "" {
		sets = append(sets, fmt.Sprintf("description=$%d", argN))
		args = append(args, req.Description)
		argN++
	}
	if req.State != nil {
		sets = append(sets, fmt.Sprintf("state=$%d", argN))
		args = append(args, *req.State)
		argN++
	}
	if req.DueDate != nil {
		if *req.DueDate == "" {
			sets = append(sets, "due_date=NULL")
		} else {
			t, err := time.Parse(time.RFC3339, *req.DueDate)
			if err != nil {
				t, _ = time.Parse("2006-01-02", *req.DueDate)
			}
			sets = append(sets, fmt.Sprintf("due_date=$%d", argN))
			args = append(args, t)
			argN++
		}
	}

	if len(sets) == 0 {
		return c.SendStatus(fiber.StatusNoContent)
	}

	sets = append(sets, "updated_at=NOW()")
	query := fmt.Sprintf("UPDATE milestones SET %s WHERE id=$%d",
		strings.Join(sets, ", "), argN)
	args = append(args, id)

	if _, err := h.db.Exec(ctx, query, args...); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to update milestone")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) DeleteMilestone(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid milestone id")
	}

	tag, err := h.db.Exec(context.Background(), `DELETE FROM milestones WHERE id = $1`, id)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "milestone not found")
	}
	return c.SendStatus(fiber.StatusNoContent)
}
