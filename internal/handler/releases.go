package handler

import (
	"context"
	"strconv"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/gofiber/fiber/v2"
)

type releaseRequest struct {
	TagName      string `json:"tag_name"`
	Title        string `json:"title"`
	Body         string `json:"body"`
	IsDraft      bool   `json:"is_draft"`
	IsPrerelease bool   `json:"is_prerelease"`
}

func (h *Handler) ListReleases(c *fiber.Ctx) error {
	owner := c.Params("owner")
	name := c.Params("name")

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "repository not found")
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT rl.id, rl.repo_id, rl.tag_name, rl.title, rl.body, rl.is_draft, rl.is_prerelease,
		        rl.author_id, u.username, rl.created_at, rl.updated_at
		 FROM releases rl JOIN users u ON u.id = rl.author_id
		 WHERE rl.repo_id = $1 ORDER BY rl.created_at DESC`, repoID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to list releases")
	}
	defer rows.Close()

	var releases []domain.Release
	for rows.Next() {
		var rl domain.Release
		if err := rows.Scan(&rl.ID, &rl.RepoID, &rl.TagName, &rl.Title, &rl.Body,
			&rl.IsDraft, &rl.IsPrerelease, &rl.AuthorID, &rl.Author, &rl.CreatedAt, &rl.UpdatedAt); err != nil {
			continue
		}
		releases = append(releases, rl)
	}
	if releases == nil {
		releases = []domain.Release{}
	}
	return writeJSON(c, fiber.StatusOK, releases)
}

func (h *Handler) GetRelease(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid release id")
	}

	var rl domain.Release
	err = h.db.QueryRow(context.Background(),
		`SELECT rl.id, rl.repo_id, rl.tag_name, rl.title, rl.body, rl.is_draft, rl.is_prerelease,
		        rl.author_id, u.username, rl.created_at, rl.updated_at
		 FROM releases rl JOIN users u ON u.id = rl.author_id
		 WHERE rl.id = $1`, id,
	).Scan(&rl.ID, &rl.RepoID, &rl.TagName, &rl.Title, &rl.Body,
		&rl.IsDraft, &rl.IsPrerelease, &rl.AuthorID, &rl.Author, &rl.CreatedAt, &rl.UpdatedAt)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "release not found")
	}
	return writeJSON(c, fiber.StatusOK, rl)
}

func (h *Handler) CreateRelease(c *fiber.Ctx) error {
	claims := getClaims(c)
	owner := c.Params("owner")
	name := c.Params("name")

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "repository not found")
	}

	var req releaseRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.TagName == "" || req.Title == "" {
		return writeError(c, fiber.StatusBadRequest, "tag_name and title are required")
	}

	var id int64
	err = h.db.QueryRow(context.Background(),
		`INSERT INTO releases (repo_id, tag_name, title, body, is_draft, is_prerelease, author_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		repoID, req.TagName, req.Title, req.Body, req.IsDraft, req.IsPrerelease, claims.UserID,
	).Scan(&id)
	if err != nil {
		return writeError(c, fiber.StatusConflict, "release with this tag already exists")
	}
	return writeJSON(c, fiber.StatusCreated, map[string]any{"id": id})
}

func (h *Handler) UpdateRelease(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid release id")
	}

	var req releaseRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}

	tag, err := h.db.Exec(context.Background(),
		`UPDATE releases SET title=$1, body=$2, is_draft=$3, is_prerelease=$4, updated_at=NOW() WHERE id=$5`,
		req.Title, req.Body, req.IsDraft, req.IsPrerelease, id)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "release not found")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) DeleteRelease(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid release id")
	}

	tag, err := h.db.Exec(context.Background(), `DELETE FROM releases WHERE id = $1`, id)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "release not found")
	}
	return c.SendStatus(fiber.StatusNoContent)
}
