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

type createIssueRequest struct {
	Title       string  `json:"title"`
	Body        string  `json:"body"`
	Priority    string  `json:"priority"`
	Type        string  `json:"type"`
	DueDate     *string `json:"due_date,omitempty"`
	StoryPoints int     `json:"story_points"`
	MilestoneID *int64  `json:"milestone_id,omitempty"`
	AssigneeID  *int64  `json:"assignee_id,omitempty"`
	LabelIDs    []int64 `json:"label_ids,omitempty"`
}

type updateIssueRequest struct {
	Title       *string `json:"title,omitempty"`
	Body        *string `json:"body,omitempty"`
	State       *string `json:"state,omitempty"`
	Priority    *string `json:"priority,omitempty"`
	Type        *string `json:"type,omitempty"`
	DueDate     *string `json:"due_date,omitempty"`
	StoryPoints *int    `json:"story_points,omitempty"`
	MilestoneID *int64  `json:"milestone_id,omitempty"`
	AssigneeID  *int64  `json:"assignee_id,omitempty"`
}

type commentRequest struct {
	Body string `json:"body"`
}

// getRepoID looks up the repo ID by owner+name.
func (h *Handler) getRepoID(owner, name string) (int64, error) {
	var repoID int64
	// Try user-owned repo first
	err := h.db.QueryRow(context.Background(),
		`SELECT r.id FROM repositories r
		 JOIN users u ON r.owner_id = u.id
		 WHERE u.username = $1 AND r.name = $2`, owner, name,
	).Scan(&repoID)
	if err == nil {
		return repoID, nil
	}
	// Try org-owned repo
	err = h.db.QueryRow(context.Background(),
		`SELECT r.id FROM repositories r
		 JOIN organizations o ON r.org_id = o.id
		 WHERE o.name = $1 AND r.name = $2`, owner, name,
	).Scan(&repoID)
	return repoID, err
}

// loadIssueLabels fetches labels for a list of issues.
func (h *Handler) loadIssueLabels(issueIDs []int64) (map[int64][]domain.Label, error) {
	result := make(map[int64][]domain.Label)
	if len(issueIDs) == 0 {
		return result, nil
	}

	placeholders := make([]string, len(issueIDs))
	args := make([]interface{}, len(issueIDs))
	for i, id := range issueIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	query := fmt.Sprintf(
		`SELECT il.issue_id, l.id, l.repo_id, l.name, l.color, l.description
		 FROM issue_labels il JOIN labels l ON l.id = il.label_id
		 WHERE il.issue_id IN (%s) ORDER BY l.name`,
		strings.Join(placeholders, ","))

	rows, err := h.db.Query(context.Background(), query, args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()

	for rows.Next() {
		var issueID int64
		var l domain.Label
		if err := rows.Scan(&issueID, &l.ID, &l.RepoID, &l.Name, &l.Color, &l.Description); err != nil {
			continue
		}
		result[issueID] = append(result[issueID], l)
	}
	return result, nil
}

func (h *Handler) ListIssues(c *fiber.Ctx) error {
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
	labelFilter := c.Query("labels")
	milestoneFilter := c.Query("milestone")
	assigneeFilter := c.Query("assignee")
	q := c.Query("q")
	sortParam := c.Query("sort")
	direction := c.Query("direction")

	// Build dynamic query
	conditions := []string{"i.repo_id = $1"}
	args := []interface{}{repoID}
	argIdx := 2

	if state != "all" {
		conditions = append(conditions, fmt.Sprintf("i.state = $%d", argIdx))
		args = append(args, state)
		argIdx++
	}

	if milestoneFilter != "" {
		mid, err := strconv.ParseInt(milestoneFilter, 10, 64)
		if err == nil {
			conditions = append(conditions, fmt.Sprintf("i.milestone_id = $%d", argIdx))
			args = append(args, mid)
			argIdx++
		}
	}

	if assigneeFilter != "" {
		conditions = append(conditions, fmt.Sprintf("au.username = $%d", argIdx))
		args = append(args, assigneeFilter)
		argIdx++
	}

	if labelFilter != "" {
		labelNames := strings.Split(labelFilter, ",")
		for _, ln := range labelNames {
			ln = strings.TrimSpace(ln)
			if ln == "" {
				continue
			}
			conditions = append(conditions, fmt.Sprintf(
				`EXISTS (SELECT 1 FROM issue_labels il2 JOIN labels l2 ON l2.id = il2.label_id
				 WHERE il2.issue_id = i.id AND l2.name = $%d)`, argIdx))
			args = append(args, ln)
			argIdx++
		}
	}

	if q != "" {
		conditions = append(conditions, fmt.Sprintf("(i.title ILIKE $%d OR i.body ILIKE $%d)", argIdx, argIdx))
		args = append(args, "%"+q+"%")
		argIdx++
	}

	// Build ORDER BY clause
	dir := "DESC"
	if strings.ToLower(direction) == "asc" {
		dir = "ASC"
	}
	var orderBy string
	switch sortParam {
	case "updated":
		orderBy = "i.updated_at " + dir
	case "priority":
		// Map priority text to numeric order: critical > high > medium > low > none
		orderBy = fmt.Sprintf(`CASE i.priority
			WHEN 'critical' THEN 0
			WHEN 'high'     THEN 1
			WHEN 'medium'   THEN 2
			WHEN 'low'      THEN 3
			ELSE 4
		END %s`, dir)
	case "comments":
		orderBy = fmt.Sprintf(`(SELECT COUNT(*) FROM issue_comments ic WHERE ic.issue_id = i.id) %s`, dir)
	default: // "created" or empty
		orderBy = "i.created_at " + dir
	}

	query := fmt.Sprintf(
		`SELECT i.id, i.repo_id, i.number, i.author_id, u.username, i.title, i.body, i.state,
		        i.priority, i.type, i.due_date, i.story_points,
		        i.milestone_id, i.assignee_id, au.username, i.created_at, i.updated_at
		 FROM issues i
		 JOIN users u ON u.id = i.author_id
		 LEFT JOIN users au ON au.id = i.assignee_id
		 WHERE %s
		 ORDER BY %s`,
		strings.Join(conditions, " AND "), orderBy)

	rows, err := h.db.Query(context.Background(), query, args...)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to list issues")
	}
	defer rows.Close()

	var issues []domain.Issue
	var issueIDs []int64
	for rows.Next() {
		var issue domain.Issue
		if err := rows.Scan(&issue.ID, &issue.RepoID, &issue.Number, &issue.AuthorID, &issue.Author,
			&issue.Title, &issue.Body, &issue.State,
			&issue.Priority, &issue.Type, &issue.DueDate, &issue.StoryPoints,
			&issue.MilestoneID, &issue.AssigneeID, &issue.Assignee,
			&issue.CreatedAt, &issue.UpdatedAt); err != nil {
			continue
		}
		issues = append(issues, issue)
		issueIDs = append(issueIDs, issue.ID)
	}

	// Load labels for all issues
	labelsMap, _ := h.loadIssueLabels(issueIDs)
	for i := range issues {
		if lbls, ok := labelsMap[issues[i].ID]; ok {
			issues[i].Labels = lbls
		}
		if issues[i].Labels == nil {
			issues[i].Labels = []domain.Label{}
		}
	}

	if issues == nil {
		issues = []domain.Issue{}
	}
	return writeJSON(c, fiber.StatusOK, issues)
}

func (h *Handler) CreateIssue(c *fiber.Ctx) error {
	claims := getClaims(c)
	owner := c.Params("owner")
	name := c.Params("name")

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "repository not found")
	}

	var req createIssueRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Title == "" {
		return writeError(c, fiber.StatusBadRequest, "title is required")
	}
	if req.Priority == "" {
		req.Priority = "medium"
	}
	if req.Type == "" {
		req.Type = "task"
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

	ctx := context.Background()
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to begin transaction")
	}
	defer tx.Rollback(ctx)

	var number int
	err = tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(number), 0) + 1 FROM issues WHERE repo_id = $1`, repoID,
	).Scan(&number)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to generate issue number")
	}

	var issueID int64
	err = tx.QueryRow(ctx,
		`INSERT INTO issues (repo_id, number, author_id, title, body, priority, type, due_date, story_points, milestone_id, assignee_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING id`,
		repoID, number, claims.UserID, req.Title, req.Body, req.Priority, req.Type, dueDate, req.StoryPoints, req.MilestoneID, req.AssigneeID,
	).Scan(&issueID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to create issue")
	}

	// Attach labels
	for _, labelID := range req.LabelIDs {
		tx.Exec(ctx,
			`INSERT INTO issue_labels (issue_id, label_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			issueID, labelID)
	}

	if err := tx.Commit(ctx); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to commit")
	}

	return writeJSON(c, fiber.StatusCreated, map[string]interface{}{"id": issueID, "number": number})
}

func (h *Handler) GetIssue(c *fiber.Ctx) error {
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

	var issue domain.Issue
	err = h.db.QueryRow(context.Background(),
		`SELECT i.id, i.repo_id, i.number, i.author_id, u.username, i.title, i.body, i.state,
		        i.priority, i.type, i.due_date, i.story_points,
		        i.milestone_id, i.assignee_id, au.username, i.created_at, i.updated_at
		 FROM issues i
		 JOIN users u ON u.id = i.author_id
		 LEFT JOIN users au ON au.id = i.assignee_id
		 WHERE i.repo_id = $1 AND i.number = $2`, repoID, number,
	).Scan(&issue.ID, &issue.RepoID, &issue.Number, &issue.AuthorID, &issue.Author,
		&issue.Title, &issue.Body, &issue.State,
		&issue.Priority, &issue.Type, &issue.DueDate, &issue.StoryPoints,
		&issue.MilestoneID, &issue.AssigneeID, &issue.Assignee,
		&issue.CreatedAt, &issue.UpdatedAt)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "issue not found")
	}

	// Load labels
	labelsMap, _ := h.loadIssueLabels([]int64{issue.ID})
	if lbls, ok := labelsMap[issue.ID]; ok {
		issue.Labels = lbls
	}
	if issue.Labels == nil {
		issue.Labels = []domain.Label{}
	}

	return writeJSON(c, fiber.StatusOK, issue)
}

func (h *Handler) UpdateIssue(c *fiber.Ctx) error {
	claims := getClaims(c)
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

	// Verify caller is issue author or repo owner
	var authorID, repoOwnerID int64
	err = h.db.QueryRow(context.Background(),
		`SELECT i.author_id, r.owner_id FROM issues i JOIN repositories r ON r.id = i.repo_id
		 WHERE i.repo_id = $1 AND i.number = $2`, repoID, number).Scan(&authorID, &repoOwnerID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "issue not found")
	}
	if claims.UserID != authorID && claims.UserID != repoOwnerID {
		return writeError(c, fiber.StatusForbidden, "only the issue author or repo owner can update this issue")
	}

	var req updateIssueRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}

	// Build dynamic update
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
	if req.State != nil {
		sets = append(sets, fmt.Sprintf("state=$%d", argN))
		args = append(args, *req.State)
		argN++
	}
	if req.Priority != nil {
		sets = append(sets, fmt.Sprintf("priority=$%d", argN))
		args = append(args, *req.Priority)
		argN++
	}
	if req.Type != nil {
		sets = append(sets, fmt.Sprintf("type=$%d", argN))
		args = append(args, *req.Type)
		argN++
	}
	if req.DueDate != nil {
		if *req.DueDate == "" {
			sets = append(sets, "due_date=NULL")
		} else {
			t, err := time.Parse(time.RFC3339, *req.DueDate)
			if err != nil {
				t, err = time.Parse("2006-01-02", *req.DueDate)
				if err != nil {
					return writeError(c, fiber.StatusBadRequest, "invalid due_date format")
				}
			}
			sets = append(sets, fmt.Sprintf("due_date=$%d", argN))
			args = append(args, t)
			argN++
		}
	}
	if req.StoryPoints != nil {
		sets = append(sets, fmt.Sprintf("story_points=$%d", argN))
		args = append(args, *req.StoryPoints)
		argN++
	}
	if req.MilestoneID != nil {
		if *req.MilestoneID == 0 {
			sets = append(sets, "milestone_id=NULL")
		} else {
			sets = append(sets, fmt.Sprintf("milestone_id=$%d", argN))
			args = append(args, *req.MilestoneID)
			argN++
		}
	}
	if req.AssigneeID != nil {
		if *req.AssigneeID == 0 {
			sets = append(sets, "assignee_id=NULL")
		} else {
			sets = append(sets, fmt.Sprintf("assignee_id=$%d", argN))
			args = append(args, *req.AssigneeID)
			argN++
		}
	}

	if len(sets) == 0 {
		return c.SendStatus(fiber.StatusNoContent)
	}

	sets = append(sets, "updated_at=NOW()")
	query := fmt.Sprintf("UPDATE issues SET %s WHERE repo_id=$%d AND number=$%d",
		strings.Join(sets, ", "), argN, argN+1)
	args = append(args, repoID, number)

	if _, err := h.db.Exec(ctx, query, args...); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to update issue")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) ListIssueComments(c *fiber.Ctx) error {
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

	rows, err := h.db.Query(context.Background(),
		`SELECT c.id, c.issue_id, c.author_id, u.username, c.body, c.created_at, c.updated_at
		 FROM issue_comments c
		 JOIN users u ON u.id = c.author_id
		 JOIN issues i ON i.id = c.issue_id
		 WHERE i.repo_id = $1 AND i.number = $2
		 ORDER BY c.created_at`, repoID, number)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to list comments")
	}
	defer rows.Close()

	var comments []domain.IssueComment
	for rows.Next() {
		var c domain.IssueComment
		if err := rows.Scan(&c.ID, &c.IssueID, &c.AuthorID, &c.Author, &c.Body, &c.CreatedAt, &c.UpdatedAt); err != nil {
			continue
		}
		comments = append(comments, c)
	}
	if comments == nil {
		comments = []domain.IssueComment{}
	}
	return writeJSON(c, fiber.StatusOK, comments)
}

func (h *Handler) CreateIssueComment(c *fiber.Ctx) error {
	claims := getClaims(c)
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

	var req commentRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Body == "" {
		return writeError(c, fiber.StatusBadRequest, "body is required")
	}

	// Get issue ID
	var issueID int64
	err = h.db.QueryRow(context.Background(),
		`SELECT id FROM issues WHERE repo_id = $1 AND number = $2`, repoID, number,
	).Scan(&issueID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "issue not found")
	}

	var commentID int64
	err = h.db.QueryRow(context.Background(),
		`INSERT INTO issue_comments (issue_id, author_id, body)
		 VALUES ($1, $2, $3) RETURNING id`,
		issueID, claims.UserID, req.Body,
	).Scan(&commentID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to create comment")
	}

	return writeJSON(c, fiber.StatusCreated, map[string]interface{}{"id": commentID})
}

func (h *Handler) UpdateIssueComment(c *fiber.Ctx) error {
	claims := getClaims(c)
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid comment id")
	}

	var req commentRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}

	tag, err := h.db.Exec(context.Background(),
		`UPDATE issue_comments SET body=$1, updated_at=NOW()
		 WHERE id=$2 AND author_id=$3`,
		req.Body, id, claims.UserID)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "comment not found")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) DeleteIssueComment(c *fiber.Ctx) error {
	claims := getClaims(c)
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid comment id")
	}

	tag, err := h.db.Exec(context.Background(),
		`DELETE FROM issue_comments WHERE id=$1 AND author_id=$2`, id, claims.UserID)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "comment not found")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

type addIssueToProjectRequest struct {
	ProjectSlug string `json:"project_slug"`
	ColumnID    int64  `json:"column_id"`
}

// AddIssueToProject creates a project_item linked to an existing issue.
// Route: POST /repos/{owner}/{name}/issues/{number}/add-to-project
func (h *Handler) AddIssueToProject(c *fiber.Ctx) error {
	claims := getClaims(c)
	owner := c.Params("owner")
	name := c.Params("name")
	number, err := strconv.Atoi(c.Params("number"))
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid issue number")
	}

	var req addIssueToProjectRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.ProjectSlug == "" {
		return writeError(c, fiber.StatusBadRequest, "project_slug is required")
	}
	if req.ColumnID == 0 {
		return writeError(c, fiber.StatusBadRequest, "column_id is required")
	}

	ctx := context.Background()

	// Look up issue
	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "repository not found")
	}

	var issue domain.Issue
	err = h.db.QueryRow(ctx,
		`SELECT id, title, type, priority, story_points FROM issues WHERE repo_id = $1 AND number = $2`,
		repoID, number,
	).Scan(&issue.ID, &issue.Title, &issue.Type, &issue.Priority, &issue.StoryPoints)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "issue not found")
	}

	// Look up project by slug, verifying the caller is a member
	project, err := h.getProjectFull(req.ProjectSlug, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "project not found or access denied")
	}

	// Require at least member role (not viewer) to add items
	if !h.requireProjectRole(c, project.ID, claims.UserID, "owner", "admin", "member") {
		return nil
	}

	// Verify the column belongs to this project
	var colExists bool
	_ = h.db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM project_columns WHERE id = $1 AND project_id = $2)`,
		req.ColumnID, project.ID,
	).Scan(&colExists)
	if !colExists {
		return writeError(c, fiber.StatusBadRequest, "column does not belong to this project")
	}

	var itemID int64
	err = h.db.QueryRow(ctx,
		`INSERT INTO project_items
		   (project_id, column_id, issue_id, title, type, priority, story_points, position)
		 VALUES ($1, $2, $3, $4, $5, $6, $7,
		         COALESCE((SELECT MAX(position) FROM project_items WHERE column_id = $2), 0) + 1)
		 RETURNING id`,
		project.ID, req.ColumnID, issue.ID, issue.Title, issue.Type, issue.Priority, issue.StoryPoints,
	).Scan(&itemID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to add issue to project")
	}

	return writeJSON(c, fiber.StatusCreated, map[string]any{"id": itemID})
}
