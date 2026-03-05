package domain

import "time"

type Webhook struct {
	ID        int64     `json:"id"`
	RepoID    int64     `json:"repo_id"`
	URL       string    `json:"url"`
	Secret    string    `json:"secret"`
	Events    []string  `json:"events"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
