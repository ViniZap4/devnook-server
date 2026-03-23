package handler

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/gofiber/fiber/v2"
)

// sprintColumns is the SELECT list for project_sprints with computed aggregate fields.
const sprintColumns = `
	ps.id, ps.project_id, ps.name, ps.goal, ps.number,
	ps.start_date, ps.end_date, ps.state, ps.velocity,
	ps.created_at, ps.updated_at,
	COALESCE((SELECT COUNT(*) FROM project_items pi WHERE pi.sprint_id = ps.id), 0) AS total_items,
	COALESCE((SELECT COUNT(*) FROM project_items pi
	           JOIN project_columns pc ON pc.id = pi.column_id
	           WHERE pi.sprint_id = ps.id AND pc.is_done = true), 0) AS done_items,
	COALESCE((SELECT SUM(pi.story_points) FROM project_items pi WHERE pi.sprint_id = ps.id), 0) AS total_points,
	COALESCE((SELECT SUM(pi.story_points) FROM project_items pi
	           JOIN project_columns pc ON pc.id = pi.column_id
	           WHERE pi.sprint_id = ps.id AND pc.is_done = true), 0) AS done_points`

func scanSprint(s *domain.ProjectSprint) []any {
	return []any{
		&s.ID, &s.ProjectID, &s.Name, &s.Goal, &s.Number,
		&s.StartDate, &s.EndDate, &s.State, &s.Velocity,
		&s.CreatedAt, &s.UpdatedAt,
		&s.TotalItems, &s.DoneItems, &s.TotalPoints, &s.DonePoints,
	}
}

func (h *Handler) ListProjectSprints(c *fiber.Ctx) error {
	claims := getClaims(c)
	slug := c.Params("projectSlug")

	project, err := h.getProjectFull(slug, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "project not found")
	}

	rows, err := h.db.Query(context.Background(),
		fmt.Sprintf(`SELECT %s FROM project_sprints ps WHERE ps.project_id = $1 ORDER BY ps.number DESC`, sprintColumns),
		project.ID,
	)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to list sprints")
	}
	defer rows.Close()

	var sprints []domain.ProjectSprint
	for rows.Next() {
		var s domain.ProjectSprint
		if err := rows.Scan(scanSprint(&s)...); err != nil {
			continue
		}
		sprints = append(sprints, s)
	}
	if sprints == nil {
		sprints = []domain.ProjectSprint{}
	}
	return writeJSON(c, fiber.StatusOK, sprints)
}

type createSprintRequest struct {
	Name      string  `json:"name"`
	Goal      string  `json:"goal"`
	StartDate *string `json:"start_date,omitempty"`
	EndDate   *string `json:"end_date,omitempty"`
}

func (h *Handler) CreateProjectSprint(c *fiber.Ctx) error {
	claims := getClaims(c)
	slug := c.Params("projectSlug")

	project, err := h.getProjectFull(slug, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "project not found")
	}

	var req createSprintRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Name == "" {
		return writeError(c, fiber.StatusBadRequest, "name is required")
	}

	startDate, err := parseOptionalDate(req.StartDate)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid start_date format")
	}
	endDate, err := parseOptionalDate(req.EndDate)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid end_date format")
	}

	var id int64
	err = h.db.QueryRow(context.Background(),
		`INSERT INTO project_sprints (project_id, name, goal, number, start_date, end_date)
		 VALUES ($1, $2, $3,
		         (SELECT COALESCE(MAX(number), 0) + 1 FROM project_sprints WHERE project_id = $1),
		         $4, $5)
		 RETURNING id`,
		project.ID, req.Name, req.Goal, startDate, endDate,
	).Scan(&id)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to create sprint")
	}
	return writeJSON(c, fiber.StatusCreated, map[string]any{"id": id})
}

func (h *Handler) GetProjectSprint(c *fiber.Ctx) error {
	claims := getClaims(c)
	slug := c.Params("projectSlug")
	sprintID, err := strconv.ParseInt(c.Params("sprintId"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid sprint id")
	}

	project, err := h.getProjectFull(slug, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "project not found")
	}

	var s domain.ProjectSprint
	err = h.db.QueryRow(context.Background(),
		fmt.Sprintf(`SELECT %s FROM project_sprints ps WHERE ps.id = $1 AND ps.project_id = $2`, sprintColumns),
		sprintID, project.ID,
	).Scan(scanSprint(&s)...)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "sprint not found")
	}
	return writeJSON(c, fiber.StatusOK, s)
}

type updateSprintRequest struct {
	Name      *string `json:"name,omitempty"`
	Goal      *string `json:"goal,omitempty"`
	StartDate *string `json:"start_date,omitempty"`
	EndDate   *string `json:"end_date,omitempty"`
}

func (h *Handler) UpdateProjectSprint(c *fiber.Ctx) error {
	claims := getClaims(c)
	slug := c.Params("projectSlug")
	sprintID, err := strconv.ParseInt(c.Params("sprintId"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid sprint id")
	}

	project, err := h.getProjectFull(slug, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "project not found")
	}

	var req updateSprintRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}

	ctx := context.Background()
	sets := []string{}
	args := []any{}
	argN := 1

	if req.Name != nil {
		sets = append(sets, fmt.Sprintf("name=$%d", argN))
		args = append(args, *req.Name)
		argN++
	}
	if req.Goal != nil {
		sets = append(sets, fmt.Sprintf("goal=$%d", argN))
		args = append(args, *req.Goal)
		argN++
	}
	if req.StartDate != nil {
		if *req.StartDate == "" {
			sets = append(sets, "start_date=NULL")
		} else {
			t, err := parseOptionalDate(req.StartDate)
			if err != nil {
				return writeError(c, fiber.StatusBadRequest, "invalid start_date format")
			}
			sets = append(sets, fmt.Sprintf("start_date=$%d", argN))
			args = append(args, t)
			argN++
		}
	}
	if req.EndDate != nil {
		if *req.EndDate == "" {
			sets = append(sets, "end_date=NULL")
		} else {
			t, err := parseOptionalDate(req.EndDate)
			if err != nil {
				return writeError(c, fiber.StatusBadRequest, "invalid end_date format")
			}
			sets = append(sets, fmt.Sprintf("end_date=$%d", argN))
			args = append(args, t)
			argN++
		}
	}

	if len(sets) == 0 {
		return c.SendStatus(fiber.StatusNoContent)
	}

	sets = append(sets, "updated_at=NOW()")
	query := fmt.Sprintf("UPDATE project_sprints SET %s WHERE id=$%d AND project_id=$%d",
		strings.Join(sets, ", "), argN, argN+1)
	args = append(args, sprintID, project.ID)

	tag, err := h.db.Exec(ctx, query, args...)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "sprint not found")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) DeleteProjectSprint(c *fiber.Ctx) error {
	claims := getClaims(c)
	slug := c.Params("projectSlug")
	sprintID, err := strconv.ParseInt(c.Params("sprintId"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid sprint id")
	}

	project, err := h.getProjectFull(slug, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "project not found")
	}

	tag, err := h.db.Exec(context.Background(),
		`DELETE FROM project_sprints WHERE id = $1 AND project_id = $2`,
		sprintID, project.ID,
	)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "sprint not found")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) StartProjectSprint(c *fiber.Ctx) error {
	claims := getClaims(c)
	slug := c.Params("projectSlug")
	sprintID, err := strconv.ParseInt(c.Params("sprintId"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid sprint id")
	}

	project, err := h.getProjectFull(slug, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "project not found")
	}

	ctx := context.Background()

	// Ensure no other active sprint exists for this project.
	var activeCount int
	err = h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM project_sprints WHERE project_id = $1 AND state = 'active' AND id != $2`,
		project.ID, sprintID,
	).Scan(&activeCount)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to check active sprints")
	}
	if activeCount > 0 {
		return writeError(c, fiber.StatusConflict, "another sprint is already active for this project")
	}

	tag, err := h.db.Exec(ctx,
		`UPDATE project_sprints
		 SET state = 'active',
		     start_date = COALESCE(start_date, NOW()),
		     updated_at = NOW()
		 WHERE id = $1 AND project_id = $2`,
		sprintID, project.ID,
	)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "sprint not found")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) CompleteProjectSprint(c *fiber.Ctx) error {
	claims := getClaims(c)
	slug := c.Params("projectSlug")
	sprintID, err := strconv.ParseInt(c.Params("sprintId"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid sprint id")
	}

	project, err := h.getProjectFull(slug, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "project not found")
	}

	ctx := context.Background()

	// Calculate velocity: sum of story_points for items sitting in done columns for this sprint.
	var velocity int
	err = h.db.QueryRow(ctx,
		`SELECT COALESCE(SUM(pi.story_points), 0)
		 FROM project_items pi
		 JOIN project_columns pc ON pc.id = pi.column_id
		 WHERE pi.sprint_id = $1 AND pc.is_done = true`,
		sprintID,
	).Scan(&velocity)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to calculate velocity")
	}

	tag, err := h.db.Exec(ctx,
		`UPDATE project_sprints
		 SET state = 'completed',
		     velocity = $1,
		     end_date = COALESCE(end_date, NOW()),
		     updated_at = NOW()
		 WHERE id = $2 AND project_id = $3`,
		velocity, sprintID, project.ID,
	)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "sprint not found")
	}
	return c.SendStatus(fiber.StatusNoContent)
}
