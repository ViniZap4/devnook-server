package handler

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"os/exec"
	"path/filepath"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/go-chi/chi/v5"
)

func (h *Handler) scanRepo(rows interface{ Scan(...any) error }) (domain.Repository, error) {
	var repo domain.Repository
	err := rows.Scan(&repo.ID, &repo.OwnerID, &repo.Owner, &repo.Name, &repo.Description, &repo.Website,
		&repo.IsPrivate, &repo.IsFork, &repo.ForkedFromID, &repo.DefaultBranch, &repo.Topics,
		&repo.StarsCount, &repo.ForksCount, &repo.OrgID, &repo.CreatedAt, &repo.UpdatedAt)
	return repo, err
}

const repoSelectColumns = `r.id, r.owner_id, COALESCE(o.name, u.username) as owner, r.name, r.description, r.website,
	r.is_private, r.is_fork, r.forked_from_id, r.default_branch, r.topics,
	r.stars_count, r.forks_count, r.org_id, r.created_at, r.updated_at`

func (h *Handler) ListRepos(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	rows, err := h.db.Query(context.Background(),
		`SELECT `+repoSelectColumns+`
		 FROM repositories r
		 JOIN users u ON r.owner_id = u.id
		 LEFT JOIN organizations o ON o.id = r.org_id
		 WHERE r.owner_id = $1
		    OR r.org_id IN (SELECT org_id FROM org_members WHERE user_id = $1)
		 ORDER BY r.updated_at DESC`, claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list repos")
		return
	}
	defer rows.Close()

	var repos []domain.Repository
	for rows.Next() {
		repo, err := h.scanRepo(rows)
		if err != nil {
			continue
		}
		repos = append(repos, repo)
	}
	if repos == nil {
		repos = []domain.Repository{}
	}
	writeJSON(w, http.StatusOK, repos)
}

type createRepoRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	IsPrivate   bool   `json:"is_private"`
}

func (h *Handler) CreateRepo(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	var req createRepoRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	var repoID int64
	err := h.db.QueryRow(context.Background(),
		`INSERT INTO repositories (owner_id, name, description, is_private)
		 VALUES ($1, $2, $3, $4) RETURNING id`,
		claims.UserID, req.Name, req.Description, req.IsPrivate,
	).Scan(&repoID)
	if err != nil {
		writeError(w, http.StatusConflict, "repository already exists")
		return
	}

	repoPath := filepath.Join(h.cfg.ReposPath, claims.Username, req.Name+".git")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create repo directory")
		return
	}
	cmd := exec.Command("git", "init", "--bare", repoPath)
	if err := cmd.Run(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to initialize git repo")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":        repoID,
		"name":      req.Name,
		"clone_url": fmt.Sprintf("%s/%s/%s.git", r.Host, claims.Username, req.Name),
	})
}

func (h *Handler) GetRepo(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")

	// Try user-owned first
	row := h.db.QueryRow(context.Background(),
		`SELECT `+repoSelectColumns+`
		 FROM repositories r JOIN users u ON r.owner_id = u.id
		 LEFT JOIN organizations o ON o.id = r.org_id
		 WHERE u.username = $1 AND r.name = $2 AND r.org_id IS NULL`, owner, name)
	repo, err := h.scanRepo(row)
	if err != nil {
		// Try org-owned
		row = h.db.QueryRow(context.Background(),
			`SELECT r.id, r.owner_id, o.name, r.name, r.description, r.website,
			        r.is_private, r.is_fork, r.forked_from_id, r.default_branch, r.topics,
			        r.stars_count, r.forks_count, r.org_id, r.created_at, r.updated_at
			 FROM repositories r
			 JOIN organizations o ON o.id = r.org_id
			 LEFT JOIN users u ON r.owner_id = u.id
			 WHERE o.name = $1 AND r.name = $2`, owner, name)
		repo, err = h.scanRepo(row)
		if err != nil {
			writeError(w, http.StatusNotFound, "repository not found")
			return
		}
	}
	writeJSON(w, http.StatusOK, repo)
}

type updateRepoRequest struct {
	Description   *string  `json:"description,omitempty"`
	Website       *string  `json:"website,omitempty"`
	IsPrivate     *bool    `json:"is_private,omitempty"`
	DefaultBranch *string  `json:"default_branch,omitempty"`
	Topics        []string `json:"topics,omitempty"`
}

func (h *Handler) UpdateRepo(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")

	if owner != claims.Username {
		writeError(w, http.StatusForbidden, "not your repository")
		return
	}

	var req updateRepoRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx := context.Background()

	// Build dynamic SET clause
	sets := []string{}
	args := []any{}
	argN := 1

	if req.Description != nil {
		sets = append(sets, fmt.Sprintf("description=$%d", argN))
		args = append(args, *req.Description)
		argN++
	}
	if req.Website != nil {
		sets = append(sets, fmt.Sprintf("website=$%d", argN))
		args = append(args, *req.Website)
		argN++
	}
	if req.IsPrivate != nil {
		sets = append(sets, fmt.Sprintf("is_private=$%d", argN))
		args = append(args, *req.IsPrivate)
		argN++
	}
	if req.DefaultBranch != nil {
		sets = append(sets, fmt.Sprintf("default_branch=$%d", argN))
		args = append(args, *req.DefaultBranch)
		argN++
	}
	if req.Topics != nil {
		sets = append(sets, fmt.Sprintf("topics=$%d", argN))
		args = append(args, req.Topics)
		argN++
	}

	if len(sets) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	sets = append(sets, "updated_at=NOW()")
	query := fmt.Sprintf("UPDATE repositories SET %s WHERE owner_id=$%d AND name=$%d",
		strings.Join(sets, ", "), argN, argN+1)
	args = append(args, claims.UserID, name)

	if _, err := h.db.Exec(ctx, query, args...); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update repository")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) DeleteRepo(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")

	if owner != claims.Username {
		writeError(w, http.StatusForbidden, "not your repository")
		return
	}

	tag, err := h.db.Exec(context.Background(),
		`DELETE FROM repositories WHERE owner_id = $1 AND name = $2`, claims.UserID, name)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	repoPath := filepath.Join(h.cfg.ReposPath, owner, name+".git")
	os.RemoveAll(repoPath)

	w.WriteHeader(http.StatusNoContent)
}
