package domain

import "time"

type Issue struct {
	ID          int64      `json:"id"`
	RepoID      int64      `json:"repo_id"`
	Number      int        `json:"number"`
	AuthorID    int64      `json:"author_id"`
	Author      string     `json:"author"`
	Title       string     `json:"title"`
	Body        string     `json:"body"`
	State       string     `json:"state"`
	Priority    string     `json:"priority"`
	Type        string     `json:"type"`
	DueDate     *time.Time `json:"due_date,omitempty"`
	StoryPoints int        `json:"story_points"`
	MilestoneID *int64     `json:"milestone_id,omitempty"`
	AssigneeID  *int64     `json:"assignee_id,omitempty"`
	Assignee    *string    `json:"assignee,omitempty"`
	Labels      []Label    `json:"labels"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type IssueComment struct {
	ID        int64     `json:"id"`
	IssueID   int64     `json:"issue_id"`
	AuthorID  int64     `json:"author_id"`
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Label struct {
	ID          int64  `json:"id"`
	RepoID      int64  `json:"repo_id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
}
