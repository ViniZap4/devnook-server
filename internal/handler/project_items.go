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

// itemColumns is the SELECT list for project_items with assignee username and column name via JOINs.
const itemColumns = `
	pi.id, pi.project_id, pi.column_id, pc.name,
	pi.swimlane_id, pi.sprint_id, pi.issue_id, pi.pr_id,
	pi.title, pi.body, pi.type, pi.priority, pi.story_points,
	pi.assignee_id, u.username,
	pi.position, pi.due_date, pi.started_at, pi.completed_at,
	i.number, i.state,
	pi.created_at, pi.updated_at`

const itemJoins = `
	FROM project_items pi
	JOIN project_columns pc ON pc.id = pi.column_id
	LEFT JOIN users u ON u.id = pi.assignee_id
	LEFT JOIN issues i ON i.id = pi.issue_id`

func scanItem(it *domain.ProjectItem) []any {
	return []any{
		&it.ID, &it.ProjectID, &it.ColumnID, &it.ColumnName,
		&it.SwimlaneID, &it.SprintID, &it.IssueID, &it.PRID,
		&it.Title, &it.Body, &it.Type, &it.Priority, &it.StoryPoints,
		&it.AssigneeID, &it.Assignee,
		&it.Position, &it.DueDate, &it.StartedAt, &it.CompletedAt,
		&it.IssueNumber, &it.IssueState,
		&it.CreatedAt, &it.UpdatedAt,
	}
}

// parseOptionalDate parses an optional RFC3339 or YYYY-MM-DD date string.
// Returns nil if the pointer itself is nil or points to an empty string.
func parseOptionalDate(s *string) (*time.Time, error) {
	if s == nil || *s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, *s)
	if err != nil {
		t, err = time.Parse("2006-01-02", *s)
		if err != nil {
			return nil, err
		}
	}
	return &t, nil
}

// requireProjectRole checks that userID holds one of the allowed roles in projectID.
// It writes a 403 response and returns false if the check fails.
func (h *Handler) requireProjectRole(c *fiber.Ctx, projectID, userID int64, allowed ...string) bool {
	role, err := h.getProjectRole(context.Background(), projectID, userID)
	if err != nil {
		writeError(c, fiber.StatusForbidden, "access denied")
		return false
	}
	for _, a := range allowed {
		if role == a {
			return true
		}
	}
	writeError(c, fiber.StatusForbidden, "insufficient permissions")
	return false
}

// getProjectFull looks up a project by slug, verifying the caller is a member.
// Returns the full Project domain object.
func (h *Handler) getProjectFull(slug string, userID int64) (domain.Project, error) {
	var p domain.Project
	err := h.db.QueryRow(context.Background(),
		`SELECT p.id, p.owner_id, u.username, p.org_id,
		        (SELECT o.name FROM organizations o WHERE o.id = p.org_id),
		        p.name, p.slug, p.description, p.methodology,
		        p.visibility, p.default_view, p.color, p.icon,
		        (SELECT COUNT(*) FROM project_members pm2 WHERE pm2.project_id = p.id),
		        (SELECT COUNT(*) FROM project_items pi WHERE pi.project_id = p.id),
		        p.created_at, p.updated_at
		 FROM projects p
		 JOIN users u ON u.id = p.owner_id
		 JOIN project_members pm ON pm.project_id = p.id
		 WHERE p.slug = $1 AND pm.user_id = $2`,
		slug, userID,
	).Scan(
		&p.ID, &p.OwnerID, &p.OwnerName, &p.OrgID, &p.OrgName,
		&p.Name, &p.Slug, &p.Description, &p.Methodology,
		&p.Visibility, &p.DefaultView, &p.Color, &p.Icon,
		&p.MemberCount, &p.ItemCount,
		&p.CreatedAt, &p.UpdatedAt,
	)
	return p, err
}

func (h *Handler) ListProjectItems(c *fiber.Ctx) error {
	claims := getClaims(c)
	slug := c.Params("projectSlug")

	project, err := h.getProjectFull(slug, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "project not found")
	}

	columnIDStr := c.Query("column_id")
	sprintIDStr := c.Query("sprint_id")
	assignee := c.Query("assignee")
	itemType := c.Query("type")
	priority := c.Query("priority")

	conditions := []string{"pi.project_id = $1"}
	args := []any{project.ID}
	argN := 2

	if columnIDStr != "" {
		cid, err := strconv.ParseInt(columnIDStr, 10, 64)
		if err == nil {
			conditions = append(conditions, fmt.Sprintf("pi.column_id = $%d", argN))
			args = append(args, cid)
			argN++
		}
	}
	if sprintIDStr != "" {
		sid, err := strconv.ParseInt(sprintIDStr, 10, 64)
		if err == nil {
			conditions = append(conditions, fmt.Sprintf("pi.sprint_id = $%d", argN))
			args = append(args, sid)
			argN++
		}
	}
	if assignee != "" {
		conditions = append(conditions, fmt.Sprintf("u.username = $%d", argN))
		args = append(args, assignee)
		argN++
	}
	if itemType != "" {
		conditions = append(conditions, fmt.Sprintf("pi.type = $%d", argN))
		args = append(args, itemType)
		argN++
	}
	if priority != "" {
		conditions = append(conditions, fmt.Sprintf("pi.priority = $%d", argN))
		args = append(args, priority)
		argN++
	}

	query := fmt.Sprintf(`SELECT %s %s WHERE %s ORDER BY pi.position ASC`,
		itemColumns, itemJoins, strings.Join(conditions, " AND "))

	rows, err := h.db.Query(context.Background(), query, args...)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to list items")
	}
	defer rows.Close()

	var items []domain.ProjectItem
	for rows.Next() {
		var it domain.ProjectItem
		if err := rows.Scan(scanItem(&it)...); err != nil {
			continue
		}
		items = append(items, it)
	}
	if items == nil {
		items = []domain.ProjectItem{}
	}
	return writeJSON(c, fiber.StatusOK, items)
}

type createItemRequest struct {
	Title       string  `json:"title"`
	Body        string  `json:"body"`
	ColumnID    int64   `json:"column_id"`
	Type        string  `json:"type"`
	Priority    string  `json:"priority"`
	StoryPoints int     `json:"story_points"`
	AssigneeID  *int64  `json:"assignee_id,omitempty"`
	SprintID    *int64  `json:"sprint_id,omitempty"`
	IssueID     *int64  `json:"issue_id,omitempty"`
	DueDate     *string `json:"due_date,omitempty"`
}

func (h *Handler) CreateProjectItem(c *fiber.Ctx) error {
	claims := getClaims(c)
	slug := c.Params("projectSlug")

	project, err := h.getProjectFull(slug, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "project not found")
	}

	if !h.requireProjectRole(c, project.ID, claims.UserID, "owner", "admin", "member") {
		return nil
	}

	var req createItemRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Title == "" {
		return writeError(c, fiber.StatusBadRequest, "title is required")
	}
	if req.ColumnID == 0 {
		return writeError(c, fiber.StatusBadRequest, "column_id is required")
	}
	if req.Type == "" {
		req.Type = "task"
	}
	if req.Priority == "" {
		req.Priority = "medium"
	}

	dueDate, err := parseOptionalDate(req.DueDate)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid due_date format")
	}

	var id int64
	err = h.db.QueryRow(context.Background(),
		`INSERT INTO project_items
		   (project_id, column_id, title, body, type, priority, story_points,
		    assignee_id, sprint_id, issue_id, due_date, position)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
		         COALESCE((SELECT MAX(position) FROM project_items WHERE column_id = $2), 0) + 1)
		 RETURNING id`,
		project.ID, req.ColumnID, req.Title, req.Body, req.Type, req.Priority, req.StoryPoints,
		req.AssigneeID, req.SprintID, req.IssueID, dueDate,
	).Scan(&id)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to create item")
	}
	return writeJSON(c, fiber.StatusCreated, map[string]any{"id": id})
}

func (h *Handler) GetProjectItem(c *fiber.Ctx) error {
	claims := getClaims(c)
	slug := c.Params("projectSlug")
	itemID, err := strconv.ParseInt(c.Params("itemId"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid item id")
	}

	project, err := h.getProjectFull(slug, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "project not found")
	}

	var it domain.ProjectItem
	err = h.db.QueryRow(context.Background(),
		fmt.Sprintf(`SELECT %s %s WHERE pi.id = $1 AND pi.project_id = $2`, itemColumns, itemJoins),
		itemID, project.ID,
	).Scan(scanItem(&it)...)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "item not found")
	}
	return writeJSON(c, fiber.StatusOK, it)
}

type updateItemRequest struct {
	Title       *string `json:"title,omitempty"`
	Body        *string `json:"body,omitempty"`
	Type        *string `json:"type,omitempty"`
	Priority    *string `json:"priority,omitempty"`
	StoryPoints *int    `json:"story_points,omitempty"`
	AssigneeID  *int64  `json:"assignee_id,omitempty"`
	SprintID    *int64  `json:"sprint_id,omitempty"`
	DueDate     *string `json:"due_date,omitempty"`
}

func (h *Handler) UpdateProjectItem(c *fiber.Ctx) error {
	claims := getClaims(c)
	slug := c.Params("projectSlug")
	itemID, err := strconv.ParseInt(c.Params("itemId"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid item id")
	}

	project, err := h.getProjectFull(slug, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "project not found")
	}

	if !h.requireProjectRole(c, project.ID, claims.UserID, "owner", "admin", "member") {
		return nil
	}

	var req updateItemRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}

	ctx := context.Background()
	sets := []string{}
	args := []any{}
	argN := 1

	if req.Title != nil {
		sets = append(sets, fmt.Sprintf("title=$%d", argN))
		args = append(args, *req.Title)
		argN++
	}
	if req.Body != nil {
		sets = append(sets, fmt.Sprintf("body=$%d", argN))
		args = append(args, *req.Body)
		argN++
	}
	if req.Type != nil {
		sets = append(sets, fmt.Sprintf("type=$%d", argN))
		args = append(args, *req.Type)
		argN++
	}
	if req.Priority != nil {
		sets = append(sets, fmt.Sprintf("priority=$%d", argN))
		args = append(args, *req.Priority)
		argN++
	}
	if req.StoryPoints != nil {
		sets = append(sets, fmt.Sprintf("story_points=$%d", argN))
		args = append(args, *req.StoryPoints)
		argN++
	}
	if req.AssigneeID != nil {
		sets = append(sets, fmt.Sprintf("assignee_id=$%d", argN))
		args = append(args, *req.AssigneeID)
		argN++
	}
	if req.SprintID != nil {
		sets = append(sets, fmt.Sprintf("sprint_id=$%d", argN))
		args = append(args, *req.SprintID)
		argN++
	}
	if req.DueDate != nil {
		if *req.DueDate == "" {
			sets = append(sets, "due_date=NULL")
		} else {
			t, err := parseOptionalDate(req.DueDate)
			if err != nil {
				return writeError(c, fiber.StatusBadRequest, "invalid due_date format")
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
	query := fmt.Sprintf("UPDATE project_items SET %s WHERE id=$%d AND project_id=$%d",
		strings.Join(sets, ", "), argN, argN+1)
	args = append(args, itemID, project.ID)

	tag, err := h.db.Exec(ctx, query, args...)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "item not found")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) DeleteProjectItem(c *fiber.Ctx) error {
	claims := getClaims(c)
	slug := c.Params("projectSlug")
	itemID, err := strconv.ParseInt(c.Params("itemId"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid item id")
	}

	project, err := h.getProjectFull(slug, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "project not found")
	}

	if !h.requireProjectRole(c, project.ID, claims.UserID, "owner", "admin") {
		return nil
	}

	tag, err := h.db.Exec(context.Background(),
		`DELETE FROM project_items WHERE id = $1 AND project_id = $2`,
		itemID, project.ID,
	)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "item not found")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

type moveItemRequest struct {
	ColumnID   int64  `json:"column_id"`
	Position   int    `json:"position"`
	SwimlaneID *int64 `json:"swimlane_id,omitempty"`
}

func (h *Handler) MoveProjectItem(c *fiber.Ctx) error {
	claims := getClaims(c)
	slug := c.Params("projectSlug")
	itemID, err := strconv.ParseInt(c.Params("itemId"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid item id")
	}

	project, err := h.getProjectFull(slug, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "project not found")
	}

	if !h.requireProjectRole(c, project.ID, claims.UserID, "owner", "admin", "member") {
		return nil
	}

	var req moveItemRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.ColumnID == 0 {
		return writeError(c, fiber.StatusBadRequest, "column_id is required")
	}

	ctx := context.Background()

	// Fetch the current column_id to determine old value for history.
	var oldColumnID int64
	var oldColumnName string
	err = h.db.QueryRow(ctx,
		`SELECT pi.column_id, pc.name FROM project_items pi
		 JOIN project_columns pc ON pc.id = pi.column_id
		 WHERE pi.id = $1 AND pi.project_id = $2`,
		itemID, project.ID,
	).Scan(&oldColumnID, &oldColumnName)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "item not found")
	}

	// Determine whether the new column is a done column.
	var newIsDone bool
	var newColumnName string
	err = h.db.QueryRow(ctx,
		`SELECT is_done, name FROM project_columns WHERE id = $1 AND project_id = $2`,
		req.ColumnID, project.ID,
	).Scan(&newIsDone, &newColumnName)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "target column not found in project")
	}

	// Build the UPDATE statement.
	now := time.Now()
	var completedAt *time.Time
	if newIsDone {
		completedAt = &now
	}

	// Check if the old column was a done column (to clear completed_at when moving away).
	var oldIsDone bool
	_ = h.db.QueryRow(ctx,
		`SELECT is_done FROM project_columns WHERE id = $1`,
		oldColumnID,
	).Scan(&oldIsDone)

	var tag interface{ RowsAffected() int64 }
	if newIsDone {
		tag, err = h.db.Exec(ctx,
			`UPDATE project_items
			 SET column_id=$1, position=$2, swimlane_id=$3, completed_at=NOW(), updated_at=NOW()
			 WHERE id=$4 AND project_id=$5`,
			req.ColumnID, req.Position, req.SwimlaneID, itemID, project.ID,
		)
	} else if oldIsDone && !newIsDone {
		tag, err = h.db.Exec(ctx,
			`UPDATE project_items
			 SET column_id=$1, position=$2, swimlane_id=$3, completed_at=NULL, updated_at=NOW()
			 WHERE id=$4 AND project_id=$5`,
			req.ColumnID, req.Position, req.SwimlaneID, itemID, project.ID,
		)
	} else {
		tag, err = h.db.Exec(ctx,
			`UPDATE project_items
			 SET column_id=$1, position=$2, swimlane_id=$3, updated_at=NOW()
			 WHERE id=$4 AND project_id=$5`,
			req.ColumnID, req.Position, req.SwimlaneID, itemID, project.ID,
		)
	}
	_ = completedAt // used via SQL NOW() above
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "item not found")
	}

	// Record in project_item_history if column changed.
	if oldColumnID != req.ColumnID {
		_, _ = h.db.Exec(ctx,
			`INSERT INTO project_item_history (item_id, user_id, field, old_value, new_value)
			 VALUES ($1, $2, 'column', $3, $4)`,
			itemID, claims.UserID, oldColumnName, newColumnName,
		)
	}

	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) GetProjectItemHistory(c *fiber.Ctx) error {
	claims := getClaims(c)
	slug := c.Params("projectSlug")
	itemID, err := strconv.ParseInt(c.Params("itemId"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid item id")
	}

	project, err := h.getProjectFull(slug, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "project not found")
	}

	// Verify the item belongs to this project.
	var exists bool
	_ = h.db.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM project_items WHERE id = $1 AND project_id = $2)`,
		itemID, project.ID,
	).Scan(&exists)
	if !exists {
		return writeError(c, fiber.StatusNotFound, "item not found")
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT h.id, h.item_id, h.user_id, u.username, h.field, h.old_value, h.new_value, h.created_at
		 FROM project_item_history h
		 JOIN users u ON u.id = h.user_id
		 WHERE h.item_id = $1
		 ORDER BY h.created_at DESC`,
		itemID,
	)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to list history")
	}
	defer rows.Close()

	var history []domain.ProjectItemHistory
	for rows.Next() {
		var entry domain.ProjectItemHistory
		if err := rows.Scan(
			&entry.ID, &entry.ItemID, &entry.UserID, &entry.Username,
			&entry.Field, &entry.OldValue, &entry.NewValue, &entry.CreatedAt,
		); err != nil {
			continue
		}
		history = append(history, entry)
	}
	if history == nil {
		history = []domain.ProjectItemHistory{}
	}
	return writeJSON(c, fiber.StatusOK, history)
}

type boardColumn struct {
	domain.ProjectColumn
	Items []domain.ProjectItem `json:"items"`
}

type boardResponse struct {
	Project      domain.Project           `json:"project"`
	Columns      []boardColumn            `json:"columns"`
	Swimlanes    []domain.ProjectSwimlane `json:"swimlanes"`
	ActiveSprint *domain.ProjectSprint    `json:"active_sprint"`
}

func (h *Handler) GetProjectBoard(c *fiber.Ctx) error {
	claims := getClaims(c)
	slug := c.Params("projectSlug")

	project, err := h.getProjectFull(slug, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "project not found")
	}

	ctx := context.Background()

	// Run columns, items, swimlanes, and active sprint queries.
	// Columns.
	colRows, err := h.db.Query(ctx,
		`SELECT id, project_id, name, color, position, wip_limit, is_done,
		        (SELECT COUNT(*) FROM project_items WHERE column_id = project_columns.id),
		        created_at
		 FROM project_columns
		 WHERE project_id = $1
		 ORDER BY position ASC`,
		project.ID,
	)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to load columns")
	}
	defer colRows.Close()

	var columns []boardColumn
	colIndex := map[int64]int{}
	for colRows.Next() {
		var col domain.ProjectColumn
		if err := colRows.Scan(
			&col.ID, &col.ProjectID, &col.Name, &col.Color,
			&col.Position, &col.WIPLimit, &col.IsDone, &col.ItemCount, &col.CreatedAt,
		); err != nil {
			continue
		}
		colIndex[col.ID] = len(columns)
		columns = append(columns, boardColumn{ProjectColumn: col, Items: []domain.ProjectItem{}})
	}
	colRows.Close()

	// Items with assignee and column name.
	itemRows, err := h.db.Query(ctx,
		fmt.Sprintf(`SELECT %s %s WHERE pi.project_id = $1 ORDER BY pi.position ASC`, itemColumns, itemJoins),
		project.ID,
	)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to load items")
	}
	defer itemRows.Close()

	for itemRows.Next() {
		var it domain.ProjectItem
		if err := itemRows.Scan(scanItem(&it)...); err != nil {
			continue
		}
		if idx, ok := colIndex[it.ColumnID]; ok {
			columns[idx].Items = append(columns[idx].Items, it)
		}
	}
	itemRows.Close()

	// Swimlanes.
	swRows, err := h.db.Query(ctx,
		`SELECT id, project_id, name, position, created_at
		 FROM project_swimlanes
		 WHERE project_id = $1
		 ORDER BY position ASC`,
		project.ID,
	)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to load swimlanes")
	}
	defer swRows.Close()

	var swimlanes []domain.ProjectSwimlane
	for swRows.Next() {
		var sw domain.ProjectSwimlane
		if err := swRows.Scan(&sw.ID, &sw.ProjectID, &sw.Name, &sw.Position, &sw.CreatedAt); err != nil {
			continue
		}
		swimlanes = append(swimlanes, sw)
	}
	swRows.Close()
	if swimlanes == nil {
		swimlanes = []domain.ProjectSwimlane{}
	}

	// Active sprint (optional).
	var activeSprint *domain.ProjectSprint
	var sp domain.ProjectSprint
	err = h.db.QueryRow(ctx,
		fmt.Sprintf(`SELECT %s FROM project_sprints ps WHERE ps.project_id = $1 AND ps.state = 'active' LIMIT 1`, sprintColumns),
		project.ID,
	).Scan(scanSprint(&sp)...)
	if err == nil {
		activeSprint = &sp
	}

	if columns == nil {
		columns = []boardColumn{}
	}

	return writeJSON(c, fiber.StatusOK, boardResponse{
		Project:      project,
		Columns:      columns,
		Swimlanes:    swimlanes,
		ActiveSprint: activeSprint,
	})
}
