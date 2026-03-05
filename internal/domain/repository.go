package domain

import "time"

type Repository struct {
	ID            int64     `json:"id"`
	OwnerID       int64     `json:"owner_id"`
	Owner         string    `json:"owner"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	Website       string    `json:"website"`
	IsPrivate     bool      `json:"is_private"`
	IsFork        bool      `json:"is_fork"`
	ForkedFromID  *int64    `json:"forked_from_id,omitempty"`
	DefaultBranch string    `json:"default_branch"`
	Topics        []string  `json:"topics"`
	StarsCount    int       `json:"stars_count"`
	ForksCount    int       `json:"forks_count"`
	OrgID         *int64    `json:"org_id,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Star struct {
	UserID    int64     `json:"user_id"`
	Username  string    `json:"username"`
	RepoID    int64     `json:"repo_id"`
	CreatedAt time.Time `json:"created_at"`
}
