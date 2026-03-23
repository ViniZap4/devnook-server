package handler

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/gofiber/fiber/v2"
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

func (h *Handler) ListRepos(c *fiber.Ctx) error {
	claims := getClaims(c)
	rows, err := h.db.Query(context.Background(),
		`SELECT `+repoSelectColumns+`
		 FROM repositories r
		 JOIN users u ON r.owner_id = u.id
		 LEFT JOIN organizations o ON o.id = r.org_id
		 WHERE r.owner_id = $1
		    OR r.org_id IN (SELECT org_id FROM org_members WHERE user_id = $1)
		 ORDER BY r.updated_at DESC`, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to list repos")
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
	return writeJSON(c, fiber.StatusOK, repos)
}

type createRepoRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	IsPrivate   bool   `json:"is_private"`
}

func (h *Handler) CreateRepo(c *fiber.Ctx) error {
	claims := getClaims(c)
	var req createRepoRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Name == "" {
		return writeError(c, fiber.StatusBadRequest, "name is required")
	}

	var repoID int64
	err := h.db.QueryRow(context.Background(),
		`INSERT INTO repositories (owner_id, name, description, is_private)
		 VALUES ($1, $2, $3, $4) RETURNING id`,
		claims.UserID, req.Name, req.Description, req.IsPrivate,
	).Scan(&repoID)
	if err != nil {
		return writeError(c, fiber.StatusConflict, "repository already exists")
	}

	repoPath := filepath.Join(h.cfg.ReposPath, claims.Username, req.Name+".git")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to create repo directory")
	}
	cmd := exec.Command("git", "init", "--bare", repoPath)
	if err := cmd.Run(); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to initialize git repo")
	}

	return writeJSON(c, fiber.StatusCreated, map[string]any{
		"id":        repoID,
		"name":      req.Name,
		"clone_url": fmt.Sprintf("%s/%s/%s.git", c.Hostname(), claims.Username, req.Name),
	})
}

func (h *Handler) GetRepo(c *fiber.Ctx) error {
	owner := c.Params("owner")
	name := c.Params("name")

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
			return writeError(c, fiber.StatusNotFound, "repository not found")
		}
	}
	return writeJSON(c, fiber.StatusOK, repo)
}

type updateRepoRequest struct {
	Description   *string  `json:"description,omitempty"`
	Website       *string  `json:"website,omitempty"`
	IsPrivate     *bool    `json:"is_private,omitempty"`
	DefaultBranch *string  `json:"default_branch,omitempty"`
	Topics        []string `json:"topics,omitempty"`
}

func (h *Handler) UpdateRepo(c *fiber.Ctx) error {
	claims := getClaims(c)
	owner := c.Params("owner")
	name := c.Params("name")

	if owner != claims.Username {
		return writeError(c, fiber.StatusForbidden, "not your repository")
	}

	var req updateRepoRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
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
		return c.SendStatus(fiber.StatusNoContent)
	}

	sets = append(sets, "updated_at=NOW()")
	query := fmt.Sprintf("UPDATE repositories SET %s WHERE owner_id=$%d AND name=$%d",
		strings.Join(sets, ", "), argN, argN+1)
	args = append(args, claims.UserID, name)

	if _, err := h.db.Exec(ctx, query, args...); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to update repository")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) DeleteRepo(c *fiber.Ctx) error {
	claims := getClaims(c)
	owner := c.Params("owner")
	name := c.Params("name")

	if owner != claims.Username {
		return writeError(c, fiber.StatusForbidden, "not your repository")
	}

	tag, err := h.db.Exec(context.Background(),
		`DELETE FROM repositories WHERE owner_id = $1 AND name = $2`, claims.UserID, name)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "repository not found")
	}

	repoPath := filepath.Join(h.cfg.ReposPath, owner, name+".git")
	os.RemoveAll(repoPath)

	return c.SendStatus(fiber.StatusNoContent)
}
