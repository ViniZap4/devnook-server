package domain

import "time"

type Issue struct {
	ID        int64     `json:"id"`
	RepoID    int64     `json:"repo_id"`
	Number    int       `json:"number"`
	AuthorID  int64     `json:"author_id"`
	Author    string    `json:"author"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
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
	ID     int64  `json:"id"`
	RepoID int64  `json:"repo_id"`
	Name   string `json:"name"`
	Color  string `json:"color"`
}
