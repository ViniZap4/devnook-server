package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

type Collaborator struct {
	ID         int64     `json:"id"`
	Username   string    `json:"username"`
	FullName   string    `json:"full_name"`
	AvatarURL  string    `json:"avatar_url"`
	Permission string    `json:"permission"`
	CreatedAt  time.Time `json:"created_at"`
}

func (h *Handler) ListCollaborators(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT u.id, u.username, u.full_name, u.avatar_url, rc.permission, rc.created_at
		 FROM repo_collaborators rc
		 JOIN users u ON u.id = rc.user_id
		 WHERE rc.repo_id = $1
		 ORDER BY rc.created_at`, repoID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list collaborators")
		return
	}
	defer rows.Close()

	var collabs []Collaborator
	for rows.Next() {
		var c Collaborator
		if err := rows.Scan(&c.ID, &c.Username, &c.FullName, &c.AvatarURL, &c.Permission, &c.CreatedAt); err != nil {
			continue
		}
		collabs = append(collabs, c)
	}
	if collabs == nil {
		collabs = []Collaborator{}
	}
	writeJSON(w, http.StatusOK, collabs)
}

func (h *Handler) AddCollaborator(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	// Verify caller owns the repo
	var ownerID int64
	if err := h.db.QueryRow(context.Background(),
		`SELECT owner_id FROM repositories WHERE id = $1`, repoID).Scan(&ownerID); err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}
	if claims.UserID != ownerID {
		writeError(w, http.StatusForbidden, "only the repository owner can manage collaborators")
		return
	}

	var req struct {
		Username   string `json:"username"`
		Permission string `json:"permission"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Username == "" {
		writeError(w, http.StatusBadRequest, "username is required")
		return
	}
	if req.Permission == "" {
		req.Permission = "write"
	}

	// Look up user
	var userID int64
	err = h.db.QueryRow(context.Background(),
		`SELECT id FROM users WHERE username = $1`, req.Username).Scan(&userID)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	_, err = h.db.Exec(context.Background(),
		`INSERT INTO repo_collaborators (repo_id, user_id, permission)
		 VALUES ($1, $2, $3) ON CONFLICT (repo_id, user_id)
		 DO UPDATE SET permission = $3`,
		repoID, userID, req.Permission)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add collaborator")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RemoveCollaborator(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")
	username := chi.URLParam(r, "username")

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	// Verify caller owns the repo
	var ownerID int64
	if err := h.db.QueryRow(context.Background(),
		`SELECT owner_id FROM repositories WHERE id = $1`, repoID).Scan(&ownerID); err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}
	if claims.UserID != ownerID {
		writeError(w, http.StatusForbidden, "only the repository owner can manage collaborators")
		return
	}

	var userID int64
	err = h.db.QueryRow(context.Background(),
		`SELECT id FROM users WHERE username = $1`, username).Scan(&userID)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	h.db.Exec(context.Background(),
		`DELETE FROM repo_collaborators WHERE repo_id = $1 AND user_id = $2`, repoID, userID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) TransferRepo(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")

	// Get current repo
	var repoID, ownerID int64
	err := h.db.QueryRow(context.Background(),
		`SELECT r.id, r.owner_id FROM repositories r
		 JOIN users u ON r.owner_id = u.id
		 WHERE u.username = $1 AND r.name = $2`, owner, name,
	).Scan(&repoID, &ownerID)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	// Only repo owner can transfer
	if claims.UserID != ownerID {
		writeError(w, http.StatusForbidden, "only the repository owner can transfer")
		return
	}

	var req struct {
		NewOwner string `json:"new_owner"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.NewOwner == "" {
		writeError(w, http.StatusBadRequest, "new_owner is required")
		return
	}

	var newOwnerID int64
	err = h.db.QueryRow(context.Background(),
		`SELECT id FROM users WHERE username = $1`, req.NewOwner).Scan(&newOwnerID)
	if err != nil {
		writeError(w, http.StatusNotFound, "new owner not found")
		return
	}

	// Check for name conflict
	var exists bool
	h.db.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM repositories WHERE owner_id = $1 AND name = $2)`,
		newOwnerID, name).Scan(&exists)
	if exists {
		writeError(w, http.StatusConflict, "the new owner already has a repository with this name")
		return
	}

	_, err = h.db.Exec(context.Background(),
		`UPDATE repositories SET owner_id = $1, updated_at = NOW() WHERE id = $2`,
		newOwnerID, repoID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to transfer repository")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"new_owner": req.NewOwner})
}
