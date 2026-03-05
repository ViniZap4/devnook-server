package domain

import "time"

type Notification struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	RepoID    *int64    `json:"repo_id,omitempty"`
	Type      string    `json:"type"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Read      bool      `json:"read"`
	Link      string    `json:"link"`
	CreatedAt time.Time `json:"created_at"`
}
