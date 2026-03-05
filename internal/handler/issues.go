package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/go-chi/chi/v5"
)

type createIssueRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type updateIssueRequest struct {
	Title *string `json:"title,omitempty"`
	Body  *string `json:"body,omitempty"`
	State *string `json:"state,omitempty"`
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

	var query string
	var args []interface{}
	if state == "all" {
		query = `SELECT i.id, i.repo_id, i.number, i.author_id, u.username, i.title, i.body, i.state, i.created_at, i.updated_at
			 FROM issues i JOIN users u ON u.id = i.author_id
			 WHERE i.repo_id = $1 ORDER BY i.created_at DESC`
		args = []interface{}{repoID}
	} else {
		query = `SELECT i.id, i.repo_id, i.number, i.author_id, u.username, i.title, i.body, i.state, i.created_at, i.updated_at
			 FROM issues i JOIN users u ON u.id = i.author_id
			 WHERE i.repo_id = $1 AND i.state = $2 ORDER BY i.created_at DESC`
		args = []interface{}{repoID, state}
	}

	rows, err := h.db.Query(context.Background(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list issues")
		return
	}
	defer rows.Close()

	var issues []domain.Issue
	for rows.Next() {
		var issue domain.Issue
		if err := rows.Scan(&issue.ID, &issue.RepoID, &issue.Number, &issue.AuthorID, &issue.Author,
			&issue.Title, &issue.Body, &issue.State, &issue.CreatedAt, &issue.UpdatedAt); err != nil {
			continue
		}
		issues = append(issues, issue)
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
		`INSERT INTO issues (repo_id, number, author_id, title, body)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		repoID, number, claims.UserID, req.Title, req.Body,
	).Scan(&issueID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create issue")
		return
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
		`SELECT i.id, i.repo_id, i.number, i.author_id, u.username, i.title, i.body, i.state, i.created_at, i.updated_at
		 FROM issues i JOIN users u ON u.id = i.author_id
		 WHERE i.repo_id = $1 AND i.number = $2`, repoID, number,
	).Scan(&issue.ID, &issue.RepoID, &issue.Number, &issue.AuthorID, &issue.Author,
		&issue.Title, &issue.Body, &issue.State, &issue.CreatedAt, &issue.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
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
