package domain

import "time"

type Milestone struct {
	ID           int64      `json:"id"`
	RepoID       int64      `json:"repo_id"`
	Title        string     `json:"title"`
	Description  string     `json:"description"`
	State        string     `json:"state"`
	DueDate      *time.Time `json:"due_date,omitempty"`
	OpenIssues   int        `json:"open_issues"`
	ClosedIssues int        `json:"closed_issues"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}
