package handler

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

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
			`SELECT id, username, email, full_name, avatar_url, bio, location, website, is_admin, created_at, updated_at
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
				&u.Bio, &u.Location, &u.Website, &u.IsAdmin, &u.CreatedAt, &u.UpdatedAt); err != nil {
				continue
			}
			users = append(users, u)
		}
	} else {
		rows, err := h.db.Query(ctx,
			`SELECT id, username, email, full_name, avatar_url, bio, location, website, is_admin, created_at, updated_at
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
				&u.Bio, &u.Location, &u.Website, &u.IsAdmin, &u.CreatedAt, &u.UpdatedAt); err != nil {
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
		`SELECT id, username, email, full_name, avatar_url, bio, location, website, is_admin, created_at, updated_at
		 FROM users WHERE username = $1`, username,
	).Scan(&u.ID, &u.Username, &u.Email, &u.FullName, &u.AvatarURL, &u.Bio, &u.Location, &u.Website, &u.IsAdmin, &u.CreatedAt, &u.UpdatedAt)
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
	sets := []string{}
	args := []any{}
	argN := 1

	if req.IsAdmin != nil {
		sets = append(sets, fmt.Sprintf("is_admin=$%d", argN))
		args = append(args, *req.IsAdmin)
		argN++
	}
	if req.FullName != nil {
		sets = append(sets, fmt.Sprintf("full_name=$%d", argN))
		args = append(args, *req.FullName)
		argN++
	}
	if req.Email != nil {
		sets = append(sets, fmt.Sprintf("email=$%d", argN))
		args = append(args, *req.Email)
		argN++
	}

	if len(sets) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	sets = append(sets, "updated_at=NOW()")
	query := fmt.Sprintf("UPDATE users SET %s WHERE username=$%d",
		strings.Join(sets, ", "), argN)
	args = append(args, username)

	if _, err := h.db.Exec(ctx, query, args...); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update user")
		return
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
		`SELECT id, name, display_name, description, avatar_url, location, website, created_at, updated_at
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
			&o.Location, &o.Website, &o.CreatedAt, &o.UpdatedAt); err != nil {
			continue
		}
		orgs = append(orgs, o)
	}
	if orgs == nil {
		orgs = []domain.Organization{}
	}
	writeJSON(w, http.StatusOK, orgs)
}

func (h *Handler) AdminAnalytics(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}

	ctx := context.Background()
	days := 30

	// User signups per day (last 30 days)
	userRows, err := h.db.Query(ctx,
		`SELECT DATE(created_at) AS day, COUNT(*) AS count
		 FROM users
		 WHERE created_at >= NOW() - INTERVAL '1 day' * $1
		 GROUP BY day ORDER BY day`, days)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get analytics")
		return
	}
	defer userRows.Close()

	type dayCount struct {
		Day   string `json:"day"`
		Count int    `json:"count"`
	}

	var userGrowth []dayCount
	for userRows.Next() {
		var dc dayCount
		var day time.Time
		if err := userRows.Scan(&day, &dc.Count); err != nil {
			continue
		}
		dc.Day = day.Format("2006-01-02")
		userGrowth = append(userGrowth, dc)
	}
	if userGrowth == nil {
		userGrowth = []dayCount{}
	}

	// Repo creations per day
	repoRows, err := h.db.Query(ctx,
		`SELECT DATE(created_at) AS day, COUNT(*) AS count
		 FROM repositories
		 WHERE created_at >= NOW() - INTERVAL '1 day' * $1
		 GROUP BY day ORDER BY day`, days)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get analytics")
		return
	}
	defer repoRows.Close()

	var repoGrowth []dayCount
	for repoRows.Next() {
		var dc dayCount
		var day time.Time
		if err := repoRows.Scan(&day, &dc.Count); err != nil {
			continue
		}
		dc.Day = day.Format("2006-01-02")
		repoGrowth = append(repoGrowth, dc)
	}
	if repoGrowth == nil {
		repoGrowth = []dayCount{}
	}

	// Issues created per day
	issueRows, err := h.db.Query(ctx,
		`SELECT DATE(created_at) AS day, COUNT(*) AS count
		 FROM issues
		 WHERE created_at >= NOW() - INTERVAL '1 day' * $1
		 GROUP BY day ORDER BY day`, days)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get analytics")
		return
	}
	defer issueRows.Close()

	var issueGrowth []dayCount
	for issueRows.Next() {
		var dc dayCount
		var day time.Time
		if err := issueRows.Scan(&day, &dc.Count); err != nil {
			continue
		}
		dc.Day = day.Format("2006-01-02")
		issueGrowth = append(issueGrowth, dc)
	}
	if issueGrowth == nil {
		issueGrowth = []dayCount{}
	}

	// Recent admin-relevant counts
	var activeToday, newThisWeek, newThisMonth int
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE updated_at >= NOW() - INTERVAL '1 day'`).Scan(&activeToday)
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE created_at >= NOW() - INTERVAL '7 days'`).Scan(&newThisWeek)
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE created_at >= NOW() - INTERVAL '30 days'`).Scan(&newThisMonth)

	writeJSON(w, http.StatusOK, map[string]any{
		"user_growth":    userGrowth,
		"repo_growth":    repoGrowth,
		"issue_growth":   issueGrowth,
		"active_today":   activeToday,
		"new_this_week":  newThisWeek,
		"new_this_month": newThisMonth,
	})
}
