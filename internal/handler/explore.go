package handler

import (
	"math"
	"strconv"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/gofiber/fiber/v2"
)

type exploreResponse struct {
	Repos      []domain.Repository `json:"repos"`
	TotalCount int                 `json:"total_count"`
	Page       int                 `json:"page"`
	PerPage    int                 `json:"per_page"`
	TotalPages int                 `json:"total_pages"`
}

func (h *Handler) ExploreRepos(c *fiber.Ctx) error {
	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	perPage := 20
	offset := (page - 1) * perPage

	q := c.Query("q")
	sort := c.Query("sort")
	if sort == "" {
		sort = "updated"
	}

	orderBy := "r.updated_at DESC"
	switch sort {
	case "name":
		orderBy = "r.name ASC"
	case "created":
		orderBy = "r.created_at DESC"
	case "stars":
		orderBy = "r.stars_count DESC"
	case "forks":
		orderBy = "r.forks_count DESC"
	}

	ctx := c.UserContext()

	var totalCount int
	if q != "" {
		h.db.QueryRow(ctx,
			`SELECT COUNT(*) FROM repositories r WHERE r.is_private = false AND (r.name ILIKE '%' || $1 || '%' OR r.description ILIKE '%' || $1 || '%')`, q,
		).Scan(&totalCount)
	} else {
		h.db.QueryRow(ctx, `SELECT COUNT(*) FROM repositories r WHERE r.is_private = false`).Scan(&totalCount)
	}

	baseSelect := `SELECT ` + repoSelectColumns + `
		FROM repositories r
		JOIN users u ON r.owner_id = u.id
		LEFT JOIN organizations o ON o.id = r.org_id
		WHERE r.is_private = false`

	var query string
	var args []any
	if q != "" {
		query = baseSelect + ` AND (r.name ILIKE '%' || $1 || '%' OR r.description ILIKE '%' || $1 || '%') ORDER BY ` + orderBy + ` LIMIT $2 OFFSET $3`
		args = []any{q, perPage, offset}
	} else {
		query = baseSelect + ` ORDER BY ` + orderBy + ` LIMIT $1 OFFSET $2`
		args = []any{perPage, offset}
	}

	rows, err := h.db.Query(ctx, query, args...)
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

	return writeJSON(c, fiber.StatusOK, exploreResponse{
		Repos:      repos,
		TotalCount: totalCount,
		Page:       page,
		PerPage:    perPage,
		TotalPages: int(math.Ceil(float64(totalCount) / float64(perPage))),
	})
}
