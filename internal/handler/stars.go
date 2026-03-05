package handler

import (
	"context"
	"net/http"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/go-chi/chi/v5"
)

func (h *Handler) StarRepo(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	ctx := context.Background()
	_, err = h.db.Exec(ctx,
		`INSERT INTO stars (user_id, repo_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		claims.UserID, repoID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to star repo")
		return
	}

	h.db.Exec(ctx,
		`UPDATE repositories SET stars_count = (SELECT COUNT(*) FROM stars WHERE repo_id = $1) WHERE id = $1`,
		repoID)

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) UnstarRepo(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	ctx := context.Background()
	h.db.Exec(ctx, `DELETE FROM stars WHERE user_id = $1 AND repo_id = $2`, claims.UserID, repoID)
	h.db.Exec(ctx,
		`UPDATE repositories SET stars_count = (SELECT COUNT(*) FROM stars WHERE repo_id = $1) WHERE id = $1`,
		repoID)

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) IsStarred(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	var count int
	h.db.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM stars WHERE user_id = $1 AND repo_id = $2`,
		claims.UserID, repoID).Scan(&count)

	writeJSON(w, http.StatusOK, map[string]bool{"starred": count > 0})
}

func (h *Handler) ListStargazers(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT s.user_id, u.username, s.repo_id, s.created_at
		 FROM stars s JOIN users u ON u.id = s.user_id
		 WHERE s.repo_id = $1 ORDER BY s.created_at DESC`, repoID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list stargazers")
		return
	}
	defer rows.Close()

	var stars []domain.Star
	for rows.Next() {
		var s domain.Star
		if err := rows.Scan(&s.UserID, &s.Username, &s.RepoID, &s.CreatedAt); err != nil {
			continue
		}
		stars = append(stars, s)
	}
	if stars == nil {
		stars = []domain.Star{}
	}
	writeJSON(w, http.StatusOK, stars)
}

func (h *Handler) ListUserStarred(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")

	var userID int64
	err := h.db.QueryRow(context.Background(),
		`SELECT id FROM users WHERE username = $1`, username).Scan(&userID)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT r.id, r.owner_id, COALESCE(o.name, u.username) as owner, r.name, r.description, r.website, r.is_private, r.is_fork, r.forked_from_id, r.default_branch, r.topics, r.stars_count, r.forks_count, r.org_id, r.created_at, r.updated_at
		 FROM repositories r
		 JOIN users u ON r.owner_id = u.id
		 LEFT JOIN organizations o ON o.id = r.org_id
		 JOIN stars s ON s.repo_id = r.id
		 WHERE s.user_id = $1
		 ORDER BY s.created_at DESC`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list starred repos")
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
