package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/go-chi/chi/v5"
)

func (h *Handler) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	claims := getClaims(r)
	var isAdmin bool
	err := h.db.QueryRow(context.Background(),
		`SELECT is_admin FROM users WHERE id = $1`, claims.UserID).Scan(&isAdmin)
	if err != nil || !isAdmin {
		writeError(w, http.StatusForbidden, "admin access required")
		return false
	}
	return true
}

func (h *Handler) AdminListUsers(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	perPage := 50
	offset := (page - 1) * perPage

	q := r.URL.Query().Get("q")
	ctx := context.Background()

	var users []domain.User
	if q != "" {
		rows, err := h.db.Query(ctx,
			`SELECT id, username, email, full_name, avatar_url, is_admin, created_at, updated_at
			 FROM users WHERE username ILIKE $1 OR email ILIKE $1 OR full_name ILIKE $1
			 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
			"%"+q+"%", perPage, offset)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list users")
			return
		}
		defer rows.Close()
		for rows.Next() {
			var u domain.User
			if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.FullName, &u.AvatarURL,
				&u.IsAdmin, &u.CreatedAt, &u.UpdatedAt); err != nil {
				continue
			}
			users = append(users, u)
		}
	} else {
		rows, err := h.db.Query(ctx,
			`SELECT id, username, email, full_name, avatar_url, is_admin, created_at, updated_at
			 FROM users ORDER BY created_at DESC LIMIT $1 OFFSET $2`,
			perPage, offset)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list users")
			return
		}
		defer rows.Close()
		for rows.Next() {
			var u domain.User
			if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.FullName, &u.AvatarURL,
				&u.IsAdmin, &u.CreatedAt, &u.UpdatedAt); err != nil {
				continue
			}
			users = append(users, u)
		}
	}

	if users == nil {
		users = []domain.User{}
	}

	var total int
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&total)

	writeJSON(w, http.StatusOK, map[string]any{
		"users":       users,
		"total_count": total,
		"page":        page,
		"per_page":    perPage,
	})
}

func (h *Handler) AdminGetUser(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}

	username := chi.URLParam(r, "username")
	var u domain.User
	err := h.db.QueryRow(context.Background(),
		`SELECT id, username, email, full_name, avatar_url, is_admin, created_at, updated_at
		 FROM users WHERE username = $1`, username,
	).Scan(&u.ID, &u.Username, &u.Email, &u.FullName, &u.AvatarURL, &u.IsAdmin, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (h *Handler) AdminUpdateUser(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}

	username := chi.URLParam(r, "username")
	var req struct {
		IsAdmin  *bool   `json:"is_admin,omitempty"`
		FullName *string `json:"full_name,omitempty"`
		Email    *string `json:"email,omitempty"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx := context.Background()
	if req.IsAdmin != nil {
		h.db.Exec(ctx, `UPDATE users SET is_admin=$1, updated_at=NOW() WHERE username=$2`, *req.IsAdmin, username)
	}
	if req.FullName != nil {
		h.db.Exec(ctx, `UPDATE users SET full_name=$1, updated_at=NOW() WHERE username=$2`, *req.FullName, username)
	}
	if req.Email != nil {
		h.db.Exec(ctx, `UPDATE users SET email=$1, updated_at=NOW() WHERE username=$2`, *req.Email, username)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) AdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}

	username := chi.URLParam(r, "username")

	claims := getClaims(r)
	var targetID int64
	h.db.QueryRow(context.Background(), `SELECT id FROM users WHERE username = $1`, username).Scan(&targetID)
	if targetID == claims.UserID {
		writeError(w, http.StatusBadRequest, "cannot delete yourself")
		return
	}

	tag, err := h.db.Exec(context.Background(), `DELETE FROM users WHERE username = $1`, username)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) AdminStats(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}

	ctx := context.Background()
	var userCount, repoCount, orgCount, issueCount int
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&userCount)
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM repositories`).Scan(&repoCount)
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM organizations`).Scan(&orgCount)
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM issues`).Scan(&issueCount)

	writeJSON(w, http.StatusOK, map[string]int{
		"total_users":  userCount,
		"total_repos":  repoCount,
		"total_orgs":   orgCount,
		"total_issues": issueCount,
	})
}

func (h *Handler) AdminListRepos(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	perPage := 50
	offset := (page - 1) * perPage
	q := r.URL.Query().Get("q")
	ctx := context.Background()

	var totalCount int
	if q != "" {
		h.db.QueryRow(ctx,
			`SELECT COUNT(*) FROM repositories r WHERE r.name ILIKE '%' || $1 || '%' OR r.description ILIKE '%' || $1 || '%'`, q,
		).Scan(&totalCount)
	} else {
		h.db.QueryRow(ctx, `SELECT COUNT(*) FROM repositories`).Scan(&totalCount)
	}

	baseSelect := `SELECT ` + repoSelectColumns + `
		FROM repositories r
		JOIN users u ON r.owner_id = u.id
		LEFT JOIN organizations o ON o.id = r.org_id`

	var query string
	var args []any
	if q != "" {
		query = baseSelect + ` WHERE r.name ILIKE '%' || $1 || '%' OR r.description ILIKE '%' || $1 || '%' ORDER BY r.updated_at DESC LIMIT $2 OFFSET $3`
		args = []any{q, perPage, offset}
	} else {
		query = baseSelect + ` ORDER BY r.updated_at DESC LIMIT $1 OFFSET $2`
		args = []any{perPage, offset}
	}

	rows, err := h.db.Query(ctx, query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list repos")
		return
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

	totalPages := (totalCount + perPage - 1) / perPage
	if totalPages < 1 {
		totalPages = 1
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"repos":       repos,
		"total_count": totalCount,
		"page":        page,
		"per_page":    perPage,
		"total_pages": totalPages,
	})
}

func (h *Handler) AdminListOrgs(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}

	ctx := context.Background()
	rows, err := h.db.Query(ctx,
		`SELECT id, name, display_name, description, avatar_url, created_at, updated_at
		 FROM organizations ORDER BY created_at DESC`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list organizations")
		return
	}
	defer rows.Close()

	var orgs []domain.Organization
	for rows.Next() {
		var o domain.Organization
		if err := rows.Scan(&o.ID, &o.Name, &o.DisplayName, &o.Description, &o.AvatarURL,
			&o.CreatedAt, &o.UpdatedAt); err != nil {
			continue
		}
		orgs = append(orgs, o)
	}
	if orgs == nil {
		orgs = []domain.Organization{}
	}
	writeJSON(w, http.StatusOK, orgs)
}
