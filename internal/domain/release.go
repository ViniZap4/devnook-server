package domain

import "time"

type Release struct {
	ID           int64     `json:"id"`
	RepoID       int64     `json:"repo_id"`
	TagName      string    `json:"tag_name"`
	Title        string    `json:"title"`
	Body         string    `json:"body"`
	IsDraft      bool      `json:"is_draft"`
	IsPrerelease bool      `json:"is_prerelease"`
	AuthorID     int64     `json:"author_id"`
	Author       string    `json:"author"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Tag struct {
	Name    string    `json:"name"`
	Hash    string    `json:"hash"`
	Message string    `json:"message,omitempty"`
	Date    time.Time `json:"date"`
}
