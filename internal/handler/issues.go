package handler

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/go-chi/chi/v5"
)

type createIssueRequest struct {
	Title       string  `json:"title"`
	Body        string  `json:"body"`
	MilestoneID *int64  `json:"milestone_id,omitempty"`
	AssigneeID  *int64  `json:"assignee_id,omitempty"`
	LabelIDs    []int64 `json:"label_ids,omitempty"`
}

type updateIssueRequest struct {
	Title       *string `json:"title,omitempty"`
	Body        *string `json:"body,omitempty"`
	State       *string `json:"state,omitempty"`
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

func (h *Handler) ListIssues(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	state := r.URL.Query().Get("state")
	if state == "" {
		state = "open"
	}
	labelFilter := r.URL.Query().Get("labels")
	milestoneFilter := r.URL.Query().Get("milestone")
	assigneeFilter := r.URL.Query().Get("assignee")
	q := r.URL.Query().Get("q")

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

	query := fmt.Sprintf(
		`SELECT i.id, i.repo_id, i.number, i.author_id, u.username, i.title, i.body, i.state,
		        i.milestone_id, i.assignee_id, au.username, i.created_at, i.updated_at
		 FROM issues i
		 JOIN users u ON u.id = i.author_id
		 LEFT JOIN users au ON au.id = i.assignee_id
		 WHERE %s
		 ORDER BY i.created_at DESC`,
		strings.Join(conditions, " AND "))

	rows, err := h.db.Query(context.Background(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list issues")
		return
	}
	defer rows.Close()

	var issues []domain.Issue
	var issueIDs []int64
	for rows.Next() {
		var issue domain.Issue
		if err := rows.Scan(&issue.ID, &issue.RepoID, &issue.Number, &issue.AuthorID, &issue.Author,
			&issue.Title, &issue.Body, &issue.State,
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
	writeJSON(w, http.StatusOK, issues)
}

func (h *Handler) CreateIssue(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	var req createIssueRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	ctx := context.Background()
	tx, err := h.db.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin transaction")
		return
	}
	defer tx.Rollback(ctx)

	var number int
	err = tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(number), 0) + 1 FROM issues WHERE repo_id = $1`, repoID,
	).Scan(&number)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate issue number")
		return
	}

	var issueID int64
	err = tx.QueryRow(ctx,
		`INSERT INTO issues (repo_id, number, author_id, title, body, milestone_id, assignee_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		repoID, number, claims.UserID, req.Title, req.Body, req.MilestoneID, req.AssigneeID,
	).Scan(&issueID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create issue")
		return
	}

	// Attach labels
	for _, labelID := range req.LabelIDs {
		tx.Exec(ctx,
			`INSERT INTO issue_labels (issue_id, label_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			issueID, labelID)
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": issueID, "number": number})
}

func (h *Handler) GetIssue(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	var issue domain.Issue
	err = h.db.QueryRow(context.Background(),
		`SELECT i.id, i.repo_id, i.number, i.author_id, u.username, i.title, i.body, i.state,
		        i.milestone_id, i.assignee_id, au.username, i.created_at, i.updated_at
		 FROM issues i
		 JOIN users u ON u.id = i.author_id
		 LEFT JOIN users au ON au.id = i.assignee_id
		 WHERE i.repo_id = $1 AND i.number = $2`, repoID, number,
	).Scan(&issue.ID, &issue.RepoID, &issue.Number, &issue.AuthorID, &issue.Author,
		&issue.Title, &issue.Body, &issue.State,
		&issue.MilestoneID, &issue.AssigneeID, &issue.Assignee,
		&issue.CreatedAt, &issue.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}

	// Load labels
	labelsMap, _ := h.loadIssueLabels([]int64{issue.ID})
	if lbls, ok := labelsMap[issue.ID]; ok {
		issue.Labels = lbls
	}
	if issue.Labels == nil {
		issue.Labels = []domain.Label{}
	}

	writeJSON(w, http.StatusOK, issue)
}

func (h *Handler) UpdateIssue(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	var req updateIssueRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Build dynamic update
	ctx := context.Background()
	if req.Title != nil {
		h.db.Exec(ctx, `UPDATE issues SET title=$1, updated_at=NOW() WHERE repo_id=$2 AND number=$3`,
			*req.Title, repoID, number)
	}
	if req.Body != nil {
		h.db.Exec(ctx, `UPDATE issues SET body=$1, updated_at=NOW() WHERE repo_id=$2 AND number=$3`,
			*req.Body, repoID, number)
	}
	if req.State != nil {
		h.db.Exec(ctx, `UPDATE issues SET state=$1, updated_at=NOW() WHERE repo_id=$2 AND number=$3`,
			*req.State, repoID, number)
	}
	if req.MilestoneID != nil {
		if *req.MilestoneID == 0 {
			h.db.Exec(ctx, `UPDATE issues SET milestone_id=NULL, updated_at=NOW() WHERE repo_id=$1 AND number=$2`,
				repoID, number)
		} else {
			h.db.Exec(ctx, `UPDATE issues SET milestone_id=$1, updated_at=NOW() WHERE repo_id=$2 AND number=$3`,
				*req.MilestoneID, repoID, number)
		}
	}
	if req.AssigneeID != nil {
		if *req.AssigneeID == 0 {
			h.db.Exec(ctx, `UPDATE issues SET assignee_id=NULL, updated_at=NOW() WHERE repo_id=$1 AND number=$2`,
				repoID, number)
		} else {
			h.db.Exec(ctx, `UPDATE issues SET assignee_id=$1, updated_at=NOW() WHERE repo_id=$2 AND number=$3`,
				*req.AssigneeID, repoID, number)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListIssueComments(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT c.id, c.issue_id, c.author_id, u.username, c.body, c.created_at, c.updated_at
		 FROM issue_comments c
		 JOIN users u ON u.id = c.author_id
		 JOIN issues i ON i.id = c.issue_id
		 WHERE i.repo_id = $1 AND i.number = $2
		 ORDER BY c.created_at`, repoID, number)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list comments")
		return
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
	writeJSON(w, http.StatusOK, comments)
}

func (h *Handler) CreateIssueComment(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	var req commentRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Body == "" {
		writeError(w, http.StatusBadRequest, "body is required")
		return
	}

	// Get issue ID
	var issueID int64
	err = h.db.QueryRow(context.Background(),
		`SELECT id FROM issues WHERE repo_id = $1 AND number = $2`, repoID, number,
	).Scan(&issueID)
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}

	var commentID int64
	err = h.db.QueryRow(context.Background(),
		`INSERT INTO issue_comments (issue_id, author_id, body)
		 VALUES ($1, $2, $3) RETURNING id`,
		issueID, claims.UserID, req.Body,
	).Scan(&commentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create comment")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": commentID})
}

func (h *Handler) UpdateIssueComment(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid comment id")
		return
	}

	var req commentRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	tag, err := h.db.Exec(context.Background(),
		`UPDATE issue_comments SET body=$1, updated_at=NOW()
		 WHERE id=$2 AND author_id=$3`,
		req.Body, id, claims.UserID)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "comment not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) DeleteIssueComment(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid comment id")
		return
	}

	tag, err := h.db.Exec(context.Background(),
		`DELETE FROM issue_comments WHERE id=$1 AND author_id=$2`, id, claims.UserID)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "comment not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
