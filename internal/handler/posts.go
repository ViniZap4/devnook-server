package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/go-chi/chi/v5"
)

func (h *Handler) GetFeed(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	ctx := context.Background()

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
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
		writeError(w, http.StatusInternalServerError, "failed to load feed")
		return
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

	writeJSON(w, http.StatusOK, map[string]any{
		"posts":       posts,
		"total_count": total,
		"page":        page,
	})
}

func (h *Handler) GetPost(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)

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
		writeError(w, http.StatusNotFound, "post not found")
		return
	}
	if p.Tags == nil {
		p.Tags = []string{}
	}
	writeJSON(w, http.StatusOK, p)
}

func (h *Handler) CreatePost(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	var req struct {
		Content     string   `json:"content"`
		Type        string   `json:"type"`
		RepoOwner   *string  `json:"repo_owner"`
		RepoName    *string  `json:"repo_name"`
		CommitHash  *string  `json:"commit_hash"`
		IssueNumber *int     `json:"issue_number"`
		Tags        []string `json:"tags"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
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
		writeError(w, http.StatusInternalServerError, "failed to create post")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

func (h *Handler) UpdatePost(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)

	var req struct {
		Content string   `json:"content"`
		Tags    []string `json:"tags"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Tags == nil {
		req.Tags = []string{}
	}

	tag, err := h.db.Exec(context.Background(),
		`UPDATE posts SET content=$1, tags=$2, updated_at=NOW() WHERE id=$3 AND author_id=$4`,
		req.Content, req.Tags, id, claims.UserID)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "post not found or not yours")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) DeletePost(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)

	tag, err := h.db.Exec(context.Background(),
		`DELETE FROM posts WHERE id=$1 AND author_id=$2`, id, claims.UserID)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "post not found or not yours")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) LikePost(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)

	_, err := h.db.Exec(context.Background(),
		`INSERT INTO post_likes (user_id, post_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		claims.UserID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to like")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) UnlikePost(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)

	h.db.Exec(context.Background(),
		`DELETE FROM post_likes WHERE user_id=$1 AND post_id=$2`, claims.UserID, id)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RepostPost(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)

	_, err := h.db.Exec(context.Background(),
		`INSERT INTO post_reposts (user_id, post_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		claims.UserID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to repost")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) GetPostComments(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	_ = claims // reserved for liked check
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)

	rows, err := h.db.Query(context.Background(),
		`SELECT c.id, c.post_id, c.author_id, u.username, u.full_name, c.content, c.created_at
		 FROM post_comments c
		 JOIN users u ON u.id = c.author_id
		 WHERE c.post_id = $1
		 ORDER BY c.created_at ASC`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get comments")
		return
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
	writeJSON(w, http.StatusOK, comments)
}

func (h *Handler) AddPostComment(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	postID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)

	var req struct {
		Content string `json:"content"`
	}
	if err := readJSON(r, &req); err != nil || req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	var id int64
	err := h.db.QueryRow(context.Background(),
		`INSERT INTO post_comments (post_id, author_id, content) VALUES ($1, $2, $3) RETURNING id`,
		postID, claims.UserID, req.Content).Scan(&id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add comment")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

func (h *Handler) RemovePostComment(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	commentID, _ := strconv.ParseInt(chi.URLParam(r, "commentId"), 10, 64)

	h.db.Exec(context.Background(),
		`DELETE FROM post_comments WHERE id=$1 AND author_id=$2`, commentID, claims.UserID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) GetUserPosts(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	username := chi.URLParam(r, "username")

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
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
		writeError(w, http.StatusInternalServerError, "failed to load posts")
		return
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

	writeJSON(w, http.StatusOK, map[string]any{
		"posts":       posts,
		"total_count": total,
	})
}
