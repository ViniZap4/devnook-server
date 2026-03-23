package handler

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
)

type Collaborator struct {
	ID         int64     `json:"id"`
	Username   string    `json:"username"`
	FullName   string    `json:"full_name"`
	AvatarURL  string    `json:"avatar_url"`
	Permission string    `json:"permission"`
	CreatedAt  time.Time `json:"created_at"`
}

func (h *Handler) ListCollaborators(c *fiber.Ctx) error {
	owner := c.Params("owner")
	name := c.Params("name")

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "repository not found")
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT u.id, u.username, u.full_name, u.avatar_url, rc.permission, rc.created_at
		 FROM repo_collaborators rc
		 JOIN users u ON u.id = rc.user_id
		 WHERE rc.repo_id = $1
		 ORDER BY rc.created_at`, repoID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to list collaborators")
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
	return writeJSON(c, fiber.StatusOK, collabs)
}

func (h *Handler) AddCollaborator(c *fiber.Ctx) error {
	claims := getClaims(c)
	owner := c.Params("owner")
	name := c.Params("name")

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "repository not found")
	}

	// Verify caller owns the repo
	var ownerID int64
	if err := h.db.QueryRow(context.Background(),
		`SELECT owner_id FROM repositories WHERE id = $1`, repoID).Scan(&ownerID); err != nil {
		return writeError(c, fiber.StatusNotFound, "repository not found")
	}
	if claims.UserID != ownerID {
		return writeError(c, fiber.StatusForbidden, "only the repository owner can manage collaborators")
	}

	var req struct {
		Username   string `json:"username"`
		Permission string `json:"permission"`
	}
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Username == "" {
		return writeError(c, fiber.StatusBadRequest, "username is required")
	}
	if req.Permission == "" {
		req.Permission = "write"
	}

	// Look up user
	var userID int64
	err = h.db.QueryRow(context.Background(),
		`SELECT id FROM users WHERE username = $1`, req.Username).Scan(&userID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "user not found")
	}

	_, err = h.db.Exec(context.Background(),
		`INSERT INTO repo_collaborators (repo_id, user_id, permission)
		 VALUES ($1, $2, $3) ON CONFLICT (repo_id, user_id)
		 DO UPDATE SET permission = $3`,
		repoID, userID, req.Permission)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to add collaborator")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) RemoveCollaborator(c *fiber.Ctx) error {
	claims := getClaims(c)
	owner := c.Params("owner")
	name := c.Params("name")
	username := c.Params("username")

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "repository not found")
	}

	// Verify caller owns the repo
	var ownerID int64
	if err := h.db.QueryRow(context.Background(),
		`SELECT owner_id FROM repositories WHERE id = $1`, repoID).Scan(&ownerID); err != nil {
		return writeError(c, fiber.StatusNotFound, "repository not found")
	}
	if claims.UserID != ownerID {
		return writeError(c, fiber.StatusForbidden, "only the repository owner can manage collaborators")
	}

	var userID int64
	err = h.db.QueryRow(context.Background(),
		`SELECT id FROM users WHERE username = $1`, username).Scan(&userID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "user not found")
	}

	h.db.Exec(context.Background(),
		`DELETE FROM repo_collaborators WHERE repo_id = $1 AND user_id = $2`, repoID, userID)
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) TransferRepo(c *fiber.Ctx) error {
	claims := getClaims(c)
	owner := c.Params("owner")
	name := c.Params("name")

	// Get current repo
	var repoID, ownerID int64
	err := h.db.QueryRow(context.Background(),
		`SELECT r.id, r.owner_id FROM repositories r
		 JOIN users u ON r.owner_id = u.id
		 WHERE u.username = $1 AND r.name = $2`, owner, name,
	).Scan(&repoID, &ownerID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "repository not found")
	}

	// Only repo owner can transfer
	if claims.UserID != ownerID {
		return writeError(c, fiber.StatusForbidden, "only the repository owner can transfer")
	}

	var req struct {
		NewOwner string `json:"new_owner"`
	}
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.NewOwner == "" {
		return writeError(c, fiber.StatusBadRequest, "new_owner is required")
	}

	var newOwnerID int64
	err = h.db.QueryRow(context.Background(),
		`SELECT id FROM users WHERE username = $1`, req.NewOwner).Scan(&newOwnerID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "new owner not found")
	}

	// Check for name conflict
	var exists bool
	h.db.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM repositories WHERE owner_id = $1 AND name = $2)`,
		newOwnerID, name).Scan(&exists)
	if exists {
		return writeError(c, fiber.StatusConflict, "the new owner already has a repository with this name")
	}

	_, err = h.db.Exec(context.Background(),
		`UPDATE repositories SET owner_id = $1, updated_at = NOW() WHERE id = $2`,
		newOwnerID, repoID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to transfer repository")
	}

	return writeJSON(c, fiber.StatusOK, map[string]string{"new_owner": req.NewOwner})
}
