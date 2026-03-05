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

func (h *Handler) ForkRepo(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")

	// Get source repo
	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	// Can't fork your own repo
	if owner == claims.Username {
		writeError(w, http.StatusBadRequest, "cannot fork your own repository")
		return
	}

	ctx := context.Background()

	// Check if fork already exists
	var existing int
	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM repositories WHERE owner_id = $1 AND name = $2`,
		claims.UserID, name).Scan(&existing)
	if existing > 0 {
		writeError(w, http.StatusConflict, "you already have a repository with this name")
		return
	}

	// Create fork record
	var forkID int64
	err = h.db.QueryRow(ctx,
		`INSERT INTO repositories (owner_id, name, description, is_private, is_fork, forked_from_id, default_branch)
		 SELECT $1, name, description, false, true, $2, default_branch
		 FROM repositories WHERE id = $2
		 RETURNING id`,
		claims.UserID, repoID,
	).Scan(&forkID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create fork")
		return
	}

	// Clone bare repo on disk
	srcPath := h.repoPath(owner, name)
	dstPath := filepath.Join(h.cfg.ReposPath, claims.Username, name+".git")
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create fork directory")
		return
	}

	cmd := exec.Command("git", "clone", "--bare", srcPath, dstPath)
	if err := cmd.Run(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clone repository")
		return
	}

	// Update forks_count on source
	h.db.Exec(ctx,
		`UPDATE repositories SET forks_count = (SELECT COUNT(*) FROM repositories WHERE forked_from_id = $1) WHERE id = $1`,
		repoID)

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":        forkID,
		"name":      name,
		"clone_url": fmt.Sprintf("%s/%s/%s.git", r.Host, claims.Username, name),
	})
}

func (h *Handler) ListForks(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT r.id, r.owner_id, u.username, r.name, r.description, r.website, r.is_private, r.is_fork, r.forked_from_id, r.default_branch, r.topics, r.stars_count, r.forks_count, r.org_id, r.created_at, r.updated_at
		 FROM repositories r JOIN users u ON r.owner_id = u.id
		 WHERE r.forked_from_id = $1
		 ORDER BY r.created_at DESC`, repoID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list forks")
		return
	}
	defer rows.Close()

	var repos []domain.Repository
	for rows.Next() {
		var repo domain.Repository
		if err := rows.Scan(&repo.ID, &repo.OwnerID, &repo.Owner, &repo.Name, &repo.Description, &repo.Website,
			&repo.IsPrivate, &repo.IsFork, &repo.ForkedFromID, &repo.DefaultBranch, &repo.Topics,
			&repo.StarsCount, &repo.ForksCount, &repo.OrgID, &repo.CreatedAt, &repo.UpdatedAt); err != nil {
			continue
		}
		repos = append(repos, repo)
	}
	if repos == nil {
		repos = []domain.Repository{}
	}
	writeJSON(w, http.StatusOK, repos)
}
