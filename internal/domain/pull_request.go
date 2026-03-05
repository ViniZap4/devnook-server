package domain

import "time"

type PullRequest struct {
	ID         int64      `json:"id"`
	RepoID     int64      `json:"repo_id"`
	Number     int        `json:"number"`
	AuthorID   int64      `json:"author_id"`
	Author     string     `json:"author"`
	Title      string     `json:"title"`
	Body       string     `json:"body"`
	State      string     `json:"state"`
	HeadBranch string     `json:"head_branch"`
	BaseBranch string     `json:"base_branch"`
	MergedAt   *time.Time `json:"merged_at,omitempty"`
	MergedBy   *int64     `json:"merged_by,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type PRComment struct {
	ID        int64     `json:"id"`
	PRID      int64     `json:"pr_id"`
	AuthorID  int64     `json:"author_id"`
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	Path      *string   `json:"path,omitempty"`
	Line      *int      `json:"line,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type PRReview struct {
	ID        int64     `json:"id"`
	PRID      int64     `json:"pr_id"`
	AuthorID  int64     `json:"author_id"`
	Author    string    `json:"author"`
	State     string    `json:"state"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}
