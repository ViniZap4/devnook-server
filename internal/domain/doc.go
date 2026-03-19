package domain

import "time"

type DocSpace struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description"`
	Icon        string    `json:"icon"`
	OwnerType   string    `json:"owner_type"`   // "user", "org", "repo"
	OwnerID     int64     `json:"owner_id"`
	OwnerName   string    `json:"owner_name"`
	RepoOwner   *string   `json:"repo_owner"`
	RepoName    *string   `json:"repo_name"`
	OrgName     *string   `json:"org_name"`
	IsPublic    bool      `json:"is_public"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type DocPage struct {
	ID             int64     `json:"id"`
	SpaceID        int64     `json:"space_id"`
	ParentID       *int64    `json:"parent_id"`
	Title          string    `json:"title"`
	Slug           string    `json:"slug"`
	Content        string    `json:"content"`
	Icon           string    `json:"icon"`
	AuthorUsername string    `json:"author_username"`
	Position       int       `json:"position"`
	IsPublished    bool      `json:"is_published"`
	LastEditedBy   string    `json:"last_edited_by"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type DocPageVersion struct {
	ID             int64     `json:"id"`
	PageID         int64     `json:"page_id"`
	Title          string    `json:"title"`
	Content        string    `json:"content"`
	AuthorUsername string    `json:"author_username"`
	CommitHash     *string   `json:"commit_hash"`
	CreatedAt      time.Time `json:"created_at"`
}
