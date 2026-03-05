package handler

import (
	"context"
	"net/http"
	"time"
)

type ActivityItem struct {
	Type      string    `json:"type"` // "commit", "issue", "pull_request", "release", "star"
	RepoOwner string    `json:"repo_owner"`
	RepoName  string    `json:"repo_name"`
	Title     string    `json:"title"`
	Author    string    `json:"author"`
	Number    int       `json:"number,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// GetDashboardActivity returns recent activity for the authenticated user.
func (h *Handler) GetDashboardActivity(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	ctx := context.Background()

	var items []ActivityItem

	// Recent issues across user repos
	rows, err := h.db.Query(ctx,
		`SELECT 'issue' as type, COALESCE(o.name, u2.username) as repo_owner, r.name as repo_name,
			i.title, u.username as author, i.number, i.created_at
		 FROM issues i
		 JOIN repositories r ON i.repo_id = r.id
		 JOIN users u ON i.author_id = u.id
		 JOIN users u2 ON r.owner_id = u2.id
		 LEFT JOIN organizations o ON o.id = r.org_id
		 WHERE (r.owner_id = $1 OR r.org_id IN (SELECT org_id FROM org_members WHERE user_id = $1))
		 ORDER BY i.created_at DESC LIMIT 10`, claims.UserID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var item ActivityItem
			if err := rows.Scan(&item.Type, &item.RepoOwner, &item.RepoName,
				&item.Title, &item.Author, &item.Number, &item.CreatedAt); err == nil {
				items = append(items, item)
			}
		}
	}

	// Recent PRs
	rows2, err := h.db.Query(ctx,
		`SELECT 'pull_request' as type, COALESCE(o.name, u2.username) as repo_owner, r.name as repo_name,
			p.title, u.username as author, p.number, p.created_at
		 FROM pull_requests p
		 JOIN repositories r ON p.repo_id = r.id
		 JOIN users u ON p.author_id = u.id
		 JOIN users u2 ON r.owner_id = u2.id
		 LEFT JOIN organizations o ON o.id = r.org_id
		 WHERE (r.owner_id = $1 OR r.org_id IN (SELECT org_id FROM org_members WHERE user_id = $1))
		 ORDER BY p.created_at DESC LIMIT 10`, claims.UserID)
	if err == nil {
		defer rows2.Close()
		for rows2.Next() {
			var item ActivityItem
			if err := rows2.Scan(&item.Type, &item.RepoOwner, &item.RepoName,
				&item.Title, &item.Author, &item.Number, &item.CreatedAt); err == nil {
				items = append(items, item)
			}
		}
	}

	// Sort by created_at descending
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j].CreatedAt.After(items[i].CreatedAt) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}

	// Limit to 20
	if len(items) > 20 {
		items = items[:20]
	}
	if items == nil {
		items = []ActivityItem{}
	}

	writeJSON(w, http.StatusOK, items)
}
