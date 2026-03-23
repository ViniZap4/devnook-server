package handler

import (
	"context"
	"strconv"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/gofiber/fiber/v2"
)

func (h *Handler) GetFeed(c *fiber.Ctx) error {
	claims := getClaims(c)
	ctx := context.Background()

	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	perPage := 20
	offset := (page - 1) * perPage

	rows, err := h.db.Query(ctx,
		`SELECT p.id, p.author_id, u.username, u.full_name, u.avatar_url,
		        p.content, p.type, p.repo_owner, p.repo_name, p.commit_hash,
		        p.issue_number, p.org_name, p.tags, p.created_at, p.updated_at,
		        (SELECT COUNT(*) FROM post_likes WHERE post_id = p.id) AS likes_count,
		        (SELECT COUNT(*) FROM post_comments WHERE post_id = p.id) AS comments_count,
		        (SELECT COUNT(*) FROM post_reposts WHERE post_id = p.id) AS reposts_count,
		        EXISTS(SELECT 1 FROM post_likes WHERE post_id = p.id AND user_id = $1) AS liked,
		        EXISTS(SELECT 1 FROM post_reposts WHERE post_id = p.id AND user_id = $1) AS reposted
		 FROM posts p
		 JOIN users u ON u.id = p.author_id
		 ORDER BY p.created_at DESC
		 LIMIT $2 OFFSET $3`, claims.UserID, perPage, offset)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to load feed")
	}
	defer rows.Close()

	posts := []domain.Post{}
	for rows.Next() {
		var p domain.Post
		if err := rows.Scan(&p.ID, &p.AuthorID, &p.AuthorUsername, &p.AuthorFullName, &p.AuthorAvatarURL,
			&p.Content, &p.Type, &p.RepoOwner, &p.RepoName, &p.CommitHash,
			&p.IssueNumber, &p.OrgName, &p.Tags, &p.CreatedAt, &p.UpdatedAt,
			&p.LikesCount, &p.CommentsCount, &p.RepostsCount, &p.Liked, &p.Reposted); err != nil {
			continue
		}
		if p.Tags == nil {
			p.Tags = []string{}
		}
		posts = append(posts, p)
	}

	var total int
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM posts`).Scan(&total)

	return writeJSON(c, fiber.StatusOK, map[string]any{
		"posts":       posts,
		"total_count": total,
		"page":        page,
	})
}

func (h *Handler) GetPost(c *fiber.Ctx) error {
	claims := getClaims(c)
	id, _ := strconv.ParseInt(c.Params("id"), 10, 64)

	var p domain.Post
	err := h.db.QueryRow(context.Background(),
		`SELECT p.id, p.author_id, u.username, u.full_name, u.avatar_url,
		        p.content, p.type, p.repo_owner, p.repo_name, p.commit_hash,
		        p.issue_number, p.org_name, p.tags, p.created_at, p.updated_at,
		        (SELECT COUNT(*) FROM post_likes WHERE post_id = p.id),
		        (SELECT COUNT(*) FROM post_comments WHERE post_id = p.id),
		        (SELECT COUNT(*) FROM post_reposts WHERE post_id = p.id),
		        EXISTS(SELECT 1 FROM post_likes WHERE post_id = p.id AND user_id = $2),
		        EXISTS(SELECT 1 FROM post_reposts WHERE post_id = p.id AND user_id = $2)
		 FROM posts p JOIN users u ON u.id = p.author_id
		 WHERE p.id = $1`, id, claims.UserID,
	).Scan(&p.ID, &p.AuthorID, &p.AuthorUsername, &p.AuthorFullName, &p.AuthorAvatarURL,
		&p.Content, &p.Type, &p.RepoOwner, &p.RepoName, &p.CommitHash,
		&p.IssueNumber, &p.OrgName, &p.Tags, &p.CreatedAt, &p.UpdatedAt,
		&p.LikesCount, &p.CommentsCount, &p.RepostsCount, &p.Liked, &p.Reposted)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "post not found")
	}
	if p.Tags == nil {
		p.Tags = []string{}
	}
	return writeJSON(c, fiber.StatusOK, p)
}

func (h *Handler) CreatePost(c *fiber.Ctx) error {
	claims := getClaims(c)
	var req struct {
		Content     string   `json:"content"`
		Type        string   `json:"type"`
		RepoOwner   *string  `json:"repo_owner"`
		RepoName    *string  `json:"repo_name"`
		CommitHash  *string  `json:"commit_hash"`
		IssueNumber *int     `json:"issue_number"`
		Tags        []string `json:"tags"`
	}
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Content == "" {
		return writeError(c, fiber.StatusBadRequest, "content is required")
	}
	if req.Type == "" {
		req.Type = "text"
	}
	if req.Tags == nil {
		req.Tags = []string{}
	}

	var id int64
	err := h.db.QueryRow(context.Background(),
		`INSERT INTO posts (author_id, content, type, repo_owner, repo_name, commit_hash, issue_number, tags)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
		claims.UserID, req.Content, req.Type, req.RepoOwner, req.RepoName, req.CommitHash, req.IssueNumber, req.Tags,
	).Scan(&id)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to create post")
	}
	return writeJSON(c, fiber.StatusCreated, map[string]int64{"id": id})
}

func (h *Handler) UpdatePost(c *fiber.Ctx) error {
	claims := getClaims(c)
	id, _ := strconv.ParseInt(c.Params("id"), 10, 64)

	var req struct {
		Content string   `json:"content"`
		Tags    []string `json:"tags"`
	}
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Tags == nil {
		req.Tags = []string{}
	}

	tag, err := h.db.Exec(context.Background(),
		`UPDATE posts SET content=$1, tags=$2, updated_at=NOW() WHERE id=$3 AND author_id=$4`,
		req.Content, req.Tags, id, claims.UserID)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "post not found or not yours")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) DeletePost(c *fiber.Ctx) error {
	claims := getClaims(c)
	id, _ := strconv.ParseInt(c.Params("id"), 10, 64)

	tag, err := h.db.Exec(context.Background(),
		`DELETE FROM posts WHERE id=$1 AND author_id=$2`, id, claims.UserID)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "post not found or not yours")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) LikePost(c *fiber.Ctx) error {
	claims := getClaims(c)
	id, _ := strconv.ParseInt(c.Params("id"), 10, 64)

	_, err := h.db.Exec(context.Background(),
		`INSERT INTO post_likes (user_id, post_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		claims.UserID, id)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to like")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) UnlikePost(c *fiber.Ctx) error {
	claims := getClaims(c)
	id, _ := strconv.ParseInt(c.Params("id"), 10, 64)

	h.db.Exec(context.Background(),
		`DELETE FROM post_likes WHERE user_id=$1 AND post_id=$2`, claims.UserID, id)
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) RepostPost(c *fiber.Ctx) error {
	claims := getClaims(c)
	id, _ := strconv.ParseInt(c.Params("id"), 10, 64)

	_, err := h.db.Exec(context.Background(),
		`INSERT INTO post_reposts (user_id, post_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		claims.UserID, id)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to repost")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) GetPostComments(c *fiber.Ctx) error {
	claims := getClaims(c)
	_ = claims // reserved for liked check
	id, _ := strconv.ParseInt(c.Params("id"), 10, 64)

	rows, err := h.db.Query(context.Background(),
		`SELECT c.id, c.post_id, c.author_id, u.username, u.full_name, c.content, c.created_at
		 FROM post_comments c
		 JOIN users u ON u.id = c.author_id
		 WHERE c.post_id = $1
		 ORDER BY c.created_at ASC`, id)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to get comments")
	}
	defer rows.Close()

	comments := []domain.PostComment{}
	for rows.Next() {
		var c domain.PostComment
		if err := rows.Scan(&c.ID, &c.PostID, &c.AuthorID, &c.AuthorUsername, &c.AuthorFullName,
			&c.Content, &c.CreatedAt); err != nil {
			continue
		}
		comments = append(comments, c)
	}
	return writeJSON(c, fiber.StatusOK, comments)
}

func (h *Handler) AddPostComment(c *fiber.Ctx) error {
	claims := getClaims(c)
	postID, _ := strconv.ParseInt(c.Params("id"), 10, 64)

	var req struct {
		Content string `json:"content"`
	}
	if err := readJSON(c, &req); err != nil || req.Content == "" {
		return writeError(c, fiber.StatusBadRequest, "content is required")
	}

	var id int64
	err := h.db.QueryRow(context.Background(),
		`INSERT INTO post_comments (post_id, author_id, content) VALUES ($1, $2, $3) RETURNING id`,
		postID, claims.UserID, req.Content).Scan(&id)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to add comment")
	}
	return writeJSON(c, fiber.StatusCreated, map[string]int64{"id": id})
}

func (h *Handler) RemovePostComment(c *fiber.Ctx) error {
	claims := getClaims(c)
	commentID, _ := strconv.ParseInt(c.Params("commentId"), 10, 64)

	h.db.Exec(context.Background(),
		`DELETE FROM post_comments WHERE id=$1 AND author_id=$2`, commentID, claims.UserID)
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) GetUserPosts(c *fiber.Ctx) error {
	claims := getClaims(c)
	username := c.Params("username")

	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	perPage := 20
	offset := (page - 1) * perPage

	rows, err := h.db.Query(context.Background(),
		`SELECT p.id, p.author_id, u.username, u.full_name, u.avatar_url,
		        p.content, p.type, p.repo_owner, p.repo_name, p.commit_hash,
		        p.issue_number, p.org_name, p.tags, p.created_at, p.updated_at,
		        (SELECT COUNT(*) FROM post_likes WHERE post_id = p.id),
		        (SELECT COUNT(*) FROM post_comments WHERE post_id = p.id),
		        (SELECT COUNT(*) FROM post_reposts WHERE post_id = p.id),
		        EXISTS(SELECT 1 FROM post_likes WHERE post_id = p.id AND user_id = $1),
		        EXISTS(SELECT 1 FROM post_reposts WHERE post_id = p.id AND user_id = $1)
		 FROM posts p JOIN users u ON u.id = p.author_id
		 WHERE u.username = $2
		 ORDER BY p.created_at DESC
		 LIMIT $3 OFFSET $4`, claims.UserID, username, perPage, offset)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to load posts")
	}
	defer rows.Close()

	posts := []domain.Post{}
	for rows.Next() {
		var p domain.Post
		if err := rows.Scan(&p.ID, &p.AuthorID, &p.AuthorUsername, &p.AuthorFullName, &p.AuthorAvatarURL,
			&p.Content, &p.Type, &p.RepoOwner, &p.RepoName, &p.CommitHash,
			&p.IssueNumber, &p.OrgName, &p.Tags, &p.CreatedAt, &p.UpdatedAt,
			&p.LikesCount, &p.CommentsCount, &p.RepostsCount, &p.Liked, &p.Reposted); err != nil {
			continue
		}
		if p.Tags == nil {
			p.Tags = []string{}
		}
		posts = append(posts, p)
	}

	var total int
	h.db.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM posts p JOIN users u ON u.id = p.author_id WHERE u.username = $1`,
		username).Scan(&total)

	return writeJSON(c, fiber.StatusOK, map[string]any{
		"posts":       posts,
		"total_count": total,
	})
}
