package handler

import (
	"context"
	"math"
	"net/http"
	"strconv"

	"github.com/ViniZap4/devnook-server/internal/domain"
)

type exploreResponse struct {
	Repos      []domain.Repository `json:"repos"`
	TotalCount int                 `json:"total_count"`
	Page       int                 `json:"page"`
	PerPage    int                 `json:"per_page"`
	TotalPages int                 `json:"total_pages"`
}

func (h *Handler) ExploreRepos(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	perPage := 20
	offset := (page - 1) * perPage

	q := r.URL.Query().Get("q")
	sort := r.URL.Query().Get("sort")
	if sort == "" {
		sort = "updated"
	}

	orderBy := "r.updated_at DESC"
	switch sort {
	case "name":
		orderBy = "r.name ASC"
	case "created":
		orderBy = "r.created_at DESC"
	case "updated":
		orderBy = "r.updated_at DESC"
	}

	ctx := context.Background()

	var totalCount int
	if q != "" {
		h.db.QueryRow(ctx,
			`SELECT COUNT(*) FROM repositories r WHERE r.is_private = false AND (r.name ILIKE '%' || $1 || '%' OR r.description ILIKE '%' || $1 || '%')`, q,
		).Scan(&totalCount)
	} else {
		h.db.QueryRow(ctx, `SELECT COUNT(*) FROM repositories r WHERE r.is_private = false`).Scan(&totalCount)
	}

	var query string
	var args []interface{}
	baseSelect := `SELECT r.id, r.owner_id, COALESCE(o.name, u.username) as owner, r.name, r.description, r.is_private, r.default_branch, r.org_id, r.created_at, r.updated_at
		FROM repositories r
		JOIN users u ON r.owner_id = u.id
		LEFT JOIN organizations o ON o.id = r.org_id
		WHERE r.is_private = false`

	if q != "" {
		query = baseSelect + ` AND (r.name ILIKE '%' || $1 || '%' OR r.description ILIKE '%' || $1 || '%') ORDER BY ` + orderBy + ` LIMIT $2 OFFSET $3`
		args = []interface{}{q, perPage, offset}
	} else {
		query = baseSelect + ` ORDER BY ` + orderBy + ` LIMIT $1 OFFSET $2`
		args = []interface{}{perPage, offset}
	}

	rows, err := h.db.Query(ctx, query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list repos")
		return
	}
	defer rows.Close()

	var repos []domain.Repository
	for rows.Next() {
		var repo domain.Repository
		if err := rows.Scan(&repo.ID, &repo.OwnerID, &repo.Owner, &repo.Name, &repo.Description,
			&repo.IsPrivate, &repo.DefaultBranch, &repo.OrgID, &repo.CreatedAt, &repo.UpdatedAt); err != nil {
			continue
		}
		repos = append(repos, repo)
	}
	if repos == nil {
		repos = []domain.Repository{}
	}

	writeJSON(w, http.StatusOK, exploreResponse{
		Repos:      repos,
		TotalCount: totalCount,
		Page:       page,
		PerPage:    perPage,
		TotalPages: int(math.Ceil(float64(totalCount) / float64(perPage))),
	})
}
