package handler

import (
	"context"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/gofiber/fiber/v2"
)

func (h *Handler) StarRepo(c *fiber.Ctx) error {
	claims := getClaims(c)
	owner := c.Params("owner")
	name := c.Params("name")

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "repository not found")
	}

	ctx := context.Background()
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to start transaction")
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx,
		`INSERT INTO stars (user_id, repo_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		claims.UserID, repoID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to star repo")
	}

	tx.Exec(ctx,
		`UPDATE repositories SET stars_count = (SELECT COUNT(*) FROM stars WHERE repo_id = $1) WHERE id = $1`,
		repoID)

	if err := tx.Commit(ctx); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to commit")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) UnstarRepo(c *fiber.Ctx) error {
	claims := getClaims(c)
	owner := c.Params("owner")
	name := c.Params("name")

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "repository not found")
	}

	ctx := context.Background()
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to start transaction")
	}
	defer tx.Rollback(ctx)

	tx.Exec(ctx, `DELETE FROM stars WHERE user_id = $1 AND repo_id = $2`, claims.UserID, repoID)
	tx.Exec(ctx,
		`UPDATE repositories SET stars_count = (SELECT COUNT(*) FROM stars WHERE repo_id = $1) WHERE id = $1`,
		repoID)

	if err := tx.Commit(ctx); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to commit")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) IsStarred(c *fiber.Ctx) error {
	claims := getClaims(c)
	owner := c.Params("owner")
	name := c.Params("name")

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "repository not found")
	}

	var count int
	h.db.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM stars WHERE user_id = $1 AND repo_id = $2`,
		claims.UserID, repoID).Scan(&count)

	return writeJSON(c, fiber.StatusOK, map[string]bool{"starred": count > 0})
}

func (h *Handler) ListStargazers(c *fiber.Ctx) error {
	owner := c.Params("owner")
	name := c.Params("name")

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "repository not found")
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT s.user_id, u.username, s.repo_id, s.created_at
		 FROM stars s JOIN users u ON u.id = s.user_id
		 WHERE s.repo_id = $1 ORDER BY s.created_at DESC`, repoID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to list stargazers")
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
	return writeJSON(c, fiber.StatusOK, stars)
}

func (h *Handler) ListUserStarred(c *fiber.Ctx) error {
	username := c.Params("username")

	var userID int64
	err := h.db.QueryRow(context.Background(),
		`SELECT id FROM users WHERE username = $1`, username).Scan(&userID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "user not found")
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
		return writeError(c, fiber.StatusInternalServerError, "failed to list starred repos")
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
	return writeJSON(c, fiber.StatusOK, repos)
}
