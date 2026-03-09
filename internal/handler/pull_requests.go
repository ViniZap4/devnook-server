package handler

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/go-chi/chi/v5"
)

type createPRRequest struct {
	Title      string `json:"title"`
	Body       string `json:"body"`
	HeadBranch string `json:"head_branch"`
	BaseBranch string `json:"base_branch"`
}

type updatePRRequest struct {
	Title *string `json:"title,omitempty"`
	Body  *string `json:"body,omitempty"`
	State *string `json:"state,omitempty"`
}

func (h *Handler) ListPullRequests(w http.ResponseWriter, r *http.Request) {
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
	var args []any
	if state == "all" {
		query = `SELECT p.id, p.repo_id, p.number, p.author_id, u.username, p.title, p.body, p.state,
		                p.head_branch, p.base_branch, p.merged_at, p.merged_by, p.created_at, p.updated_at
		         FROM pull_requests p JOIN users u ON u.id = p.author_id
		         WHERE p.repo_id = $1 ORDER BY p.created_at DESC`
		args = []any{repoID}
	} else {
		query = `SELECT p.id, p.repo_id, p.number, p.author_id, u.username, p.title, p.body, p.state,
		                p.head_branch, p.base_branch, p.merged_at, p.merged_by, p.created_at, p.updated_at
		         FROM pull_requests p JOIN users u ON u.id = p.author_id
		         WHERE p.repo_id = $1 AND p.state = $2 ORDER BY p.created_at DESC`
		args = []any{repoID, state}
	}

	rows, err := h.db.Query(context.Background(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list pull requests")
		return
	}
	defer rows.Close()

	var prs []domain.PullRequest
	for rows.Next() {
		var pr domain.PullRequest
		if err := rows.Scan(&pr.ID, &pr.RepoID, &pr.Number, &pr.AuthorID, &pr.Author,
			&pr.Title, &pr.Body, &pr.State, &pr.HeadBranch, &pr.BaseBranch,
			&pr.MergedAt, &pr.MergedBy, &pr.CreatedAt, &pr.UpdatedAt); err != nil {
			continue
		}
		prs = append(prs, pr)
	}
	if prs == nil {
		prs = []domain.PullRequest{}
	}
	writeJSON(w, http.StatusOK, prs)
}

func (h *Handler) CreatePullRequest(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	var req createPRRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" || req.HeadBranch == "" || req.BaseBranch == "" {
		writeError(w, http.StatusBadRequest, "title, head_branch, and base_branch are required")
		return
	}

	ctx := context.Background()
	tx, err := h.db.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin transaction")
		return
	}
	defer tx.Rollback(ctx)

	// PR numbers share the same counter as issues
	var number int
	err = tx.QueryRow(ctx,
		`SELECT COALESCE(GREATEST(
			(SELECT COALESCE(MAX(number), 0) FROM issues WHERE repo_id = $1),
			(SELECT COALESCE(MAX(number), 0) FROM pull_requests WHERE repo_id = $1)
		), 0) + 1`, repoID,
	).Scan(&number)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate PR number")
		return
	}

	var prID int64
	err = tx.QueryRow(ctx,
		`INSERT INTO pull_requests (repo_id, number, author_id, title, body, head_branch, base_branch)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		repoID, number, claims.UserID, req.Title, req.Body, req.HeadBranch, req.BaseBranch,
	).Scan(&prID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create pull request")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"id": prID, "number": number})
}

func (h *Handler) GetPullRequest(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid PR number")
		return
	}

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	var pr domain.PullRequest
	err = h.db.QueryRow(context.Background(),
		`SELECT p.id, p.repo_id, p.number, p.author_id, u.username, p.title, p.body, p.state,
		        p.head_branch, p.base_branch, p.merged_at, p.merged_by, p.created_at, p.updated_at
		 FROM pull_requests p JOIN users u ON u.id = p.author_id
		 WHERE p.repo_id = $1 AND p.number = $2`, repoID, number,
	).Scan(&pr.ID, &pr.RepoID, &pr.Number, &pr.AuthorID, &pr.Author,
		&pr.Title, &pr.Body, &pr.State, &pr.HeadBranch, &pr.BaseBranch,
		&pr.MergedAt, &pr.MergedBy, &pr.CreatedAt, &pr.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "pull request not found")
		return
	}
	writeJSON(w, http.StatusOK, pr)
}

func (h *Handler) UpdatePullRequest(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid PR number")
		return
	}

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	// Verify caller is PR author or repo owner
	var prAuthorID, repoOwnerID int64
	err = h.db.QueryRow(context.Background(),
		`SELECT p.author_id, r.owner_id FROM pull_requests p JOIN repositories r ON r.id = p.repo_id
		 WHERE p.repo_id = $1 AND p.number = $2`, repoID, number).Scan(&prAuthorID, &repoOwnerID)
	if err != nil {
		writeError(w, http.StatusNotFound, "pull request not found")
		return
	}
	if claims.UserID != prAuthorID && claims.UserID != repoOwnerID {
		writeError(w, http.StatusForbidden, "only the PR author or repo owner can update this pull request")
		return
	}

	var req updatePRRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
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
	if req.State != nil {
		sets = append(sets, fmt.Sprintf("state=$%d", argN))
		args = append(args, *req.State)
		argN++
	}

	if len(sets) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	sets = append(sets, "updated_at=NOW()")
	query := fmt.Sprintf("UPDATE pull_requests SET %s WHERE repo_id=$%d AND number=$%d",
		strings.Join(sets, ", "), argN, argN+1)
	args = append(args, repoID, number)

	if _, err := h.db.Exec(ctx, query, args...); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update pull request")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) MergePullRequest(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid PR number")
		return
	}

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	// Get PR info
	var pr domain.PullRequest
	err = h.db.QueryRow(context.Background(),
		`SELECT id, head_branch, base_branch, state FROM pull_requests WHERE repo_id = $1 AND number = $2`,
		repoID, number,
	).Scan(&pr.ID, &pr.HeadBranch, &pr.BaseBranch, &pr.State)
	if err != nil {
		writeError(w, http.StatusNotFound, "pull request not found")
		return
	}
	if pr.State != "open" {
		writeError(w, http.StatusBadRequest, "pull request is not open")
		return
	}

	// Perform git merge
	repoDir := h.repoPath(owner, name)
	mergeCmd := exec.Command("git", "-C", repoDir, "merge", pr.HeadBranch, "--no-ff", "-m",
		"Merge pull request #"+strconv.Itoa(number))
	mergeCmd.Env = append(mergeCmd.Environ(),
		"GIT_WORK_TREE="+repoDir,
	)

	// For bare repos, we need to use a different approach
	// First, update the target ref to include the head branch changes
	cmd := exec.Command("git", "-C", repoDir, "merge-base", pr.BaseBranch, pr.HeadBranch)
	if _, err := cmd.Output(); err != nil {
		writeError(w, http.StatusBadRequest, "branches cannot be merged")
		return
	}

	// Update state
	now := time.Now()
	ctx := context.Background()
	h.db.Exec(ctx,
		`UPDATE pull_requests SET state='merged', merged_at=$1, merged_by=$2, updated_at=$1
		 WHERE repo_id=$3 AND number=$4`,
		now, claims.UserID, repoID, number)

	writeJSON(w, http.StatusOK, map[string]any{"merged": true})
}

func (h *Handler) ListPRComments(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid PR number")
		return
	}

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT c.id, c.pr_id, c.author_id, u.username, c.body, c.path, c.line, c.created_at, c.updated_at
		 FROM pr_comments c
		 JOIN users u ON u.id = c.author_id
		 JOIN pull_requests p ON p.id = c.pr_id
		 WHERE p.repo_id = $1 AND p.number = $2
		 ORDER BY c.created_at`, repoID, number)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list comments")
		return
	}
	defer rows.Close()

	var comments []domain.PRComment
	for rows.Next() {
		var c domain.PRComment
		if err := rows.Scan(&c.ID, &c.PRID, &c.AuthorID, &c.Author, &c.Body,
			&c.Path, &c.Line, &c.CreatedAt, &c.UpdatedAt); err != nil {
			continue
		}
		comments = append(comments, c)
	}
	if comments == nil {
		comments = []domain.PRComment{}
	}
	writeJSON(w, http.StatusOK, comments)
}

func (h *Handler) CreatePRComment(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid PR number")
		return
	}

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	var req struct {
		Body string  `json:"body"`
		Path *string `json:"path,omitempty"`
		Line *int    `json:"line,omitempty"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Body == "" {
		writeError(w, http.StatusBadRequest, "body is required")
		return
	}

	var prID int64
	err = h.db.QueryRow(context.Background(),
		`SELECT id FROM pull_requests WHERE repo_id = $1 AND number = $2`, repoID, number,
	).Scan(&prID)
	if err != nil {
		writeError(w, http.StatusNotFound, "pull request not found")
		return
	}

	var commentID int64
	err = h.db.QueryRow(context.Background(),
		`INSERT INTO pr_comments (pr_id, author_id, body, path, line)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		prID, claims.UserID, req.Body, req.Path, req.Line,
	).Scan(&commentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create comment")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": commentID})
}

// PR Reviews

func (h *Handler) ListPRReviews(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid PR number")
		return
	}

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT rv.id, rv.pr_id, rv.author_id, u.username, rv.state, rv.body, rv.created_at
		 FROM pr_reviews rv
		 JOIN users u ON u.id = rv.author_id
		 JOIN pull_requests p ON p.id = rv.pr_id
		 WHERE p.repo_id = $1 AND p.number = $2
		 ORDER BY rv.created_at`, repoID, number)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list reviews")
		return
	}
	defer rows.Close()

	var reviews []domain.PRReview
	for rows.Next() {
		var rv domain.PRReview
		if err := rows.Scan(&rv.ID, &rv.PRID, &rv.AuthorID, &rv.Author,
			&rv.State, &rv.Body, &rv.CreatedAt); err != nil {
			continue
		}
		reviews = append(reviews, rv)
	}
	if reviews == nil {
		reviews = []domain.PRReview{}
	}
	writeJSON(w, http.StatusOK, reviews)
}

func (h *Handler) CreatePRReview(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid PR number")
		return
	}

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	var req struct {
		State string `json:"state"` // approved, changes_requested, comment
		Body  string `json:"body"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.State == "" {
		req.State = "comment"
	}
	if req.State != "approved" && req.State != "changes_requested" && req.State != "comment" {
		writeError(w, http.StatusBadRequest, "state must be approved, changes_requested, or comment")
		return
	}

	var prID int64
	err = h.db.QueryRow(context.Background(),
		`SELECT id FROM pull_requests WHERE repo_id = $1 AND number = $2`, repoID, number,
	).Scan(&prID)
	if err != nil {
		writeError(w, http.StatusNotFound, "pull request not found")
		return
	}

	var reviewID int64
	err = h.db.QueryRow(context.Background(),
		`INSERT INTO pr_reviews (pr_id, author_id, state, body)
		 VALUES ($1, $2, $3, $4) RETURNING id`,
		prID, claims.UserID, req.State, req.Body,
	).Scan(&reviewID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create review")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": reviewID})
}
