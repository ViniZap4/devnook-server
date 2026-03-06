package domain

import "time"

type Post struct {
	ID              int64    `json:"id"`
	AuthorID        int64    `json:"author_id"`
	AuthorUsername  string   `json:"author_username"`
	AuthorFullName  string   `json:"author_full_name"`
	AuthorAvatarURL string   `json:"author_avatar_url"`
	Content         string   `json:"content"`
	Type            string   `json:"type"`
	RepoOwner       *string  `json:"repo_owner"`
	RepoName        *string  `json:"repo_name"`
	CommitHash      *string  `json:"commit_hash"`
	IssueNumber     *int     `json:"issue_number"`
	OrgName         *string  `json:"org_name"`
	LikesCount      int      `json:"likes_count"`
	CommentsCount   int      `json:"comments_count"`
	RepostsCount    int      `json:"reposts_count"`
	Liked           bool     `json:"liked"`
	Reposted        bool     `json:"reposted"`
	Tags            []string `json:"tags"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type PostComment struct {
	ID             int64     `json:"id"`
	PostID         int64     `json:"post_id"`
	AuthorID       int64     `json:"author_id"`
	AuthorUsername string    `json:"author_username"`
	AuthorFullName string    `json:"author_full_name"`
	Content        string    `json:"content"`
	LikesCount     int       `json:"likes_count"`
	Liked          bool      `json:"liked"`
	CreatedAt      time.Time `json:"created_at"`
}
