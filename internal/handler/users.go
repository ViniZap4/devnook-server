package handler

import (
	"context"
	"net/http"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/go-chi/chi/v5"
)

type userProfileResponse struct {
	User  domain.User              `json:"user"`
	Repos []domain.Repository      `json:"repos"`
	Orgs  []domain.Organization    `json:"orgs"`
}

type updateProfileRequest struct {
	FullName  string `json:"full_name"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar_url"`
	Bio       string `json:"bio"`
	Location  string `json:"location"`
	Website   string `json:"website"`
}

type dashboardStatsResponse struct {
	TotalRepos   int `json:"total_repos"`
	TotalOrgs    int `json:"total_orgs"`
	OpenIssues   int `json:"open_issues"`
	TotalCommits int `json:"total_commits"`
}

func (h *Handler) GetUserProfile(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")

	var user domain.User
	err := h.db.QueryRow(context.Background(),
		`SELECT id, username, email, full_name, avatar_url, is_admin, created_at, updated_at
		 FROM users WHERE username = $1`, username,
	).Scan(&user.ID, &user.Username, &user.Email, &user.FullName, &user.AvatarURL, &user.IsAdmin, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT `+repoSelectColumns+`
		 FROM repositories r JOIN users u ON r.owner_id = u.id
		 LEFT JOIN organizations o ON o.id = r.org_id
		 WHERE u.username = $1 AND r.is_private = false AND r.org_id IS NULL
		 ORDER BY r.updated_at DESC`, username)
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

	// Fetch user's organizations
	orgRows, err := h.db.Query(context.Background(),
		`SELECT o.id, o.name, o.display_name, o.description, o.avatar_url, o.created_at, o.updated_at
		 FROM organizations o
		 JOIN org_members om ON om.org_id = o.id
		 JOIN users u ON u.id = om.user_id
		 WHERE u.username = $1
		 ORDER BY o.name`, username)
	var orgs []domain.Organization
	if err == nil {
		defer orgRows.Close()
		for orgRows.Next() {
			var org domain.Organization
			if err := orgRows.Scan(&org.ID, &org.Name, &org.DisplayName, &org.Description,
				&org.AvatarURL, &org.CreatedAt, &org.UpdatedAt); err != nil {
				continue
			}
			orgs = append(orgs, org)
		}
	}
	if orgs == nil {
		orgs = []domain.Organization{}
	}

	writeJSON(w, http.StatusOK, userProfileResponse{User: user, Repos: repos, Orgs: orgs})
}

func (h *Handler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	var req updateProfileRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	tag, err := h.db.Exec(context.Background(),
		`UPDATE users SET full_name=$1, email=$2, avatar_url=$3, updated_at=NOW()
		 WHERE id=$4`,
		req.FullName, req.Email, req.AvatarURL, claims.UserID)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusInternalServerError, "failed to update profile")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) GetDashboardStats(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	ctx := context.Background()

	var stats dashboardStatsResponse

	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM repositories WHERE owner_id = $1 OR org_id IN (SELECT org_id FROM org_members WHERE user_id = $1)`,
		claims.UserID).Scan(&stats.TotalRepos)

	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM org_members WHERE user_id = $1`, claims.UserID).Scan(&stats.TotalOrgs)

	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM issues WHERE author_id = $1 AND state = 'open'`, claims.UserID).Scan(&stats.OpenIssues)

	writeJSON(w, http.StatusOK, stats)
}
