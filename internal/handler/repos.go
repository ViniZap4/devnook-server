package handler

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/go-chi/chi/v5"
)

func (h *Handler) ListRepos(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	rows, err := h.db.Query(context.Background(),
		`SELECT r.id, r.owner_id, u.username, r.name, r.description, r.is_private, r.created_at, r.updated_at
		 FROM repositories r JOIN users u ON r.owner_id = u.id
		 WHERE r.owner_id = $1 ORDER BY r.updated_at DESC`, claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list repos")
		return
	}
	defer rows.Close()

	var repos []domain.Repository
	for rows.Next() {
		var repo domain.Repository
		if err := rows.Scan(&repo.ID, &repo.OwnerID, &repo.Owner, &repo.Name, &repo.Description, &repo.IsPrivate, &repo.CreatedAt, &repo.UpdatedAt); err != nil {
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

	// Initialize bare git repo on disk
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

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":        repoID,
		"name":      req.Name,
		"clone_url": fmt.Sprintf("%s/%s/%s.git", r.Host, claims.Username, req.Name),
	})
}

func (h *Handler) GetRepo(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")

	var repo domain.Repository
	err := h.db.QueryRow(context.Background(),
		`SELECT r.id, r.owner_id, u.username, r.name, r.description, r.is_private, r.created_at, r.updated_at
		 FROM repositories r JOIN users u ON r.owner_id = u.id
		 WHERE u.username = $1 AND r.name = $2`, owner, name,
	).Scan(&repo.ID, &repo.OwnerID, &repo.Owner, &repo.Name, &repo.Description, &repo.IsPrivate, &repo.CreatedAt, &repo.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}
	writeJSON(w, http.StatusOK, repo)
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

	// Remove bare repo from disk
	repoPath := filepath.Join(h.cfg.ReposPath, owner, name+".git")
	os.RemoveAll(repoPath)

	w.WriteHeader(http.StatusNoContent)
}
