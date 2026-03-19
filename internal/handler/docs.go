package handler

import (
	"context"
	"net/http"
	"regexp"
	"strings"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/go-chi/chi/v5"
)

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func toSlug(s string) string {
	slug := slugRe.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "-")
	return strings.Trim(slug, "-")
}

// --- Spaces ---

func (h *Handler) ListDocSpaces(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	rows, err := h.db.Query(context.Background(),
		`SELECT s.id, s.name, s.slug, s.description, s.icon, s.owner_type,
		        s.owner_id, s.owner_name, s.repo_owner, s.repo_name, s.org_name,
		        s.is_public, s.created_at, s.updated_at
		 FROM doc_spaces s
		 WHERE s.owner_id = $1 OR s.is_public = true
		 ORDER BY s.updated_at DESC`, claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list spaces")
		return
	}
	defer rows.Close()

	var spaces []domain.DocSpace
	for rows.Next() {
		var s domain.DocSpace
		if err := rows.Scan(&s.ID, &s.Name, &s.Slug, &s.Description, &s.Icon,
			&s.OwnerType, &s.OwnerID, &s.OwnerName, &s.RepoOwner, &s.RepoName,
			&s.OrgName, &s.IsPublic, &s.CreatedAt, &s.UpdatedAt); err != nil {
			continue
		}
		spaces = append(spaces, s)
	}
	if spaces == nil {
		spaces = []domain.DocSpace{}
	}
	writeJSON(w, http.StatusOK, spaces)
}

func (h *Handler) GetDocSpace(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "spaceSlug")
	claims := getClaims(r)

	var s domain.DocSpace
	err := h.db.QueryRow(context.Background(),
		`SELECT id, name, slug, description, icon, owner_type,
		        owner_id, owner_name, repo_owner, repo_name, org_name,
		        is_public, created_at, updated_at
		 FROM doc_spaces
		 WHERE slug = $1 AND (owner_id = $2 OR is_public = true)`, slug, claims.UserID).
		Scan(&s.ID, &s.Name, &s.Slug, &s.Description, &s.Icon,
			&s.OwnerType, &s.OwnerID, &s.OwnerName, &s.RepoOwner, &s.RepoName,
			&s.OrgName, &s.IsPublic, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "space not found")
		return
	}
	writeJSON(w, http.StatusOK, s)
}

type createSpaceRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	OwnerType   string `json:"owner_type"`
	RepoOwner   string `json:"repo_owner"`
	RepoName    string `json:"repo_name"`
	OrgName     string `json:"org_name"`
	IsPublic    bool   `json:"is_public"`
}

func (h *Handler) CreateDocSpace(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	var req createSpaceRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	ownerType := req.OwnerType
	if ownerType == "" {
		ownerType = "user"
	}

	slug := toSlug(req.Name)

	var id int64
	var resultSlug string
	err := h.db.QueryRow(context.Background(),
		`INSERT INTO doc_spaces (name, slug, description, icon, owner_type, owner_id, owner_name, repo_owner, repo_name, org_name, is_public)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8,''), NULLIF($9,''), NULLIF($10,''), $11)
		 RETURNING id, slug`,
		req.Name, slug, req.Description, req.Icon, ownerType,
		claims.UserID, claims.Username, req.RepoOwner, req.RepoName, req.OrgName, req.IsPublic,
	).Scan(&id, &resultSlug)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create space")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id, "slug": resultSlug})
}

type updateSpaceRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Icon        *string `json:"icon"`
	IsPublic    *bool   `json:"is_public"`
}

func (h *Handler) UpdateDocSpace(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "spaceSlug")
	claims := getClaims(r)

	var req updateSpaceRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	tag, err := h.db.Exec(context.Background(),
		`UPDATE doc_spaces SET
			name = COALESCE($1, name),
			description = COALESCE($2, description),
			icon = COALESCE($3, icon),
			is_public = COALESCE($4, is_public),
			updated_at = NOW()
		 WHERE slug = $5 AND owner_id = $6`,
		req.Name, req.Description, req.Icon, req.IsPublic, slug, claims.UserID)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "space not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) DeleteDocSpace(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "spaceSlug")
	claims := getClaims(r)

	tag, err := h.db.Exec(context.Background(),
		`DELETE FROM doc_spaces WHERE slug = $1 AND owner_id = $2`, slug, claims.UserID)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "space not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Pages ---

func (h *Handler) ListDocPages(w http.ResponseWriter, r *http.Request) {
	spaceSlug := chi.URLParam(r, "spaceSlug")
	claims := getClaims(r)

	// Verify space access
	var spaceID int64
	err := h.db.QueryRow(context.Background(),
		`SELECT id FROM doc_spaces WHERE slug = $1 AND (owner_id = $2 OR is_public = true)`,
		spaceSlug, claims.UserID).Scan(&spaceID)
	if err != nil {
		writeError(w, http.StatusNotFound, "space not found")
		return
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT p.id, p.space_id, p.parent_id, p.title, p.slug, p.content, p.icon,
		        u.username, p.position, p.is_published,
		        COALESCE(e.username, u.username), p.created_at, p.updated_at
		 FROM doc_pages p
		 JOIN users u ON u.id = p.author_id
		 LEFT JOIN users e ON e.id = p.last_edited_by
		 WHERE p.space_id = $1
		 ORDER BY p.position, p.created_at`, spaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list pages")
		return
	}
	defer rows.Close()

	var pages []domain.DocPage
	for rows.Next() {
		var p domain.DocPage
		if err := rows.Scan(&p.ID, &p.SpaceID, &p.ParentID, &p.Title, &p.Slug,
			&p.Content, &p.Icon, &p.AuthorUsername, &p.Position, &p.IsPublished,
			&p.LastEditedBy, &p.CreatedAt, &p.UpdatedAt); err != nil {
			continue
		}
		pages = append(pages, p)
	}
	if pages == nil {
		pages = []domain.DocPage{}
	}
	writeJSON(w, http.StatusOK, pages)
}

func (h *Handler) GetDocPage(w http.ResponseWriter, r *http.Request) {
	spaceSlug := chi.URLParam(r, "spaceSlug")
	pageSlug := chi.URLParam(r, "pageSlug")
	claims := getClaims(r)

	var p domain.DocPage
	err := h.db.QueryRow(context.Background(),
		`SELECT p.id, p.space_id, p.parent_id, p.title, p.slug, p.content, p.icon,
		        u.username, p.position, p.is_published,
		        COALESCE(e.username, u.username), p.created_at, p.updated_at
		 FROM doc_pages p
		 JOIN doc_spaces s ON s.id = p.space_id
		 JOIN users u ON u.id = p.author_id
		 LEFT JOIN users e ON e.id = p.last_edited_by
		 WHERE s.slug = $1 AND p.slug = $2 AND (s.owner_id = $3 OR s.is_public = true)`,
		spaceSlug, pageSlug, claims.UserID).
		Scan(&p.ID, &p.SpaceID, &p.ParentID, &p.Title, &p.Slug, &p.Content,
			&p.Icon, &p.AuthorUsername, &p.Position, &p.IsPublished,
			&p.LastEditedBy, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "page not found")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

type createPageRequest struct {
	Title    string `json:"title"`
	Content  string `json:"content"`
	Icon     string `json:"icon"`
	ParentID *int64 `json:"parent_id"`
}

func (h *Handler) CreateDocPage(w http.ResponseWriter, r *http.Request) {
	spaceSlug := chi.URLParam(r, "spaceSlug")
	claims := getClaims(r)

	var req createPageRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	// Get space
	var spaceID int64
	err := h.db.QueryRow(context.Background(),
		`SELECT id FROM doc_spaces WHERE slug = $1 AND owner_id = $2`,
		spaceSlug, claims.UserID).Scan(&spaceID)
	if err != nil {
		writeError(w, http.StatusNotFound, "space not found")
		return
	}

	slug := toSlug(req.Title)

	// Get next position
	var maxPos int
	_ = h.db.QueryRow(context.Background(),
		`SELECT COALESCE(MAX(position), -1) FROM doc_pages WHERE space_id = $1`,
		spaceID).Scan(&maxPos)

	var id int64
	var resultSlug string
	err = h.db.QueryRow(context.Background(),
		`INSERT INTO doc_pages (space_id, parent_id, title, slug, content, icon, author_id, last_edited_by, position)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $7, $8)
		 RETURNING id, slug`,
		spaceID, req.ParentID, req.Title, slug, req.Content, req.Icon,
		claims.UserID, maxPos+1,
	).Scan(&id, &resultSlug)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create page")
		return
	}

	// Update space timestamp
	h.db.Exec(context.Background(),
		`UPDATE doc_spaces SET updated_at = NOW() WHERE id = $1`, spaceID)

	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id, "slug": resultSlug})
}

type updatePageRequest struct {
	Title       *string `json:"title"`
	Content     *string `json:"content"`
	Icon        *string `json:"icon"`
	Position    *int    `json:"position"`
	IsPublished *bool   `json:"is_published"`
}

func (h *Handler) UpdateDocPage(w http.ResponseWriter, r *http.Request) {
	spaceSlug := chi.URLParam(r, "spaceSlug")
	pageSlug := chi.URLParam(r, "pageSlug")
	claims := getClaims(r)

	var req updatePageRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Get page ID and current content for versioning
	var pageID int64
	var oldTitle, oldContent string
	err := h.db.QueryRow(context.Background(),
		`SELECT p.id, p.title, p.content
		 FROM doc_pages p
		 JOIN doc_spaces s ON s.id = p.space_id
		 WHERE s.slug = $1 AND p.slug = $2 AND s.owner_id = $3`,
		spaceSlug, pageSlug, claims.UserID).Scan(&pageID, &oldTitle, &oldContent)
	if err != nil {
		writeError(w, http.StatusNotFound, "page not found")
		return
	}

	// Save version before updating
	h.db.Exec(context.Background(),
		`INSERT INTO doc_page_versions (page_id, title, content, author_id)
		 VALUES ($1, $2, $3, $4)`,
		pageID, oldTitle, oldContent, claims.UserID)

	tag, err := h.db.Exec(context.Background(),
		`UPDATE doc_pages SET
			title = COALESCE($1, title),
			content = COALESCE($2, content),
			icon = COALESCE($3, icon),
			position = COALESCE($4, position),
			is_published = COALESCE($5, is_published),
			last_edited_by = $6,
			updated_at = NOW()
		 WHERE id = $7`,
		req.Title, req.Content, req.Icon, req.Position, req.IsPublished,
		claims.UserID, pageID)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusInternalServerError, "failed to update page")
		return
	}

	// Update space timestamp
	h.db.Exec(context.Background(),
		`UPDATE doc_spaces SET updated_at = NOW()
		 WHERE id = (SELECT space_id FROM doc_pages WHERE id = $1)`, pageID)

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) DeleteDocPage(w http.ResponseWriter, r *http.Request) {
	spaceSlug := chi.URLParam(r, "spaceSlug")
	pageSlug := chi.URLParam(r, "pageSlug")
	claims := getClaims(r)

	tag, err := h.db.Exec(context.Background(),
		`DELETE FROM doc_pages p
		 USING doc_spaces s
		 WHERE p.space_id = s.id AND s.slug = $1 AND p.slug = $2 AND s.owner_id = $3`,
		spaceSlug, pageSlug, claims.UserID)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "page not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Page Versions ---

func (h *Handler) ListDocPageVersions(w http.ResponseWriter, r *http.Request) {
	spaceSlug := chi.URLParam(r, "spaceSlug")
	pageSlug := chi.URLParam(r, "pageSlug")
	claims := getClaims(r)

	rows, err := h.db.Query(context.Background(),
		`SELECT v.id, v.page_id, v.title, v.content, u.username, v.commit_hash, v.created_at
		 FROM doc_page_versions v
		 JOIN doc_pages p ON p.id = v.page_id
		 JOIN doc_spaces s ON s.id = p.space_id
		 JOIN users u ON u.id = v.author_id
		 WHERE s.slug = $1 AND p.slug = $2 AND (s.owner_id = $3 OR s.is_public = true)
		 ORDER BY v.created_at DESC`,
		spaceSlug, pageSlug, claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list versions")
		return
	}
	defer rows.Close()

	var versions []domain.DocPageVersion
	for rows.Next() {
		var v domain.DocPageVersion
		if err := rows.Scan(&v.ID, &v.PageID, &v.Title, &v.Content,
			&v.AuthorUsername, &v.CommitHash, &v.CreatedAt); err != nil {
			continue
		}
		versions = append(versions, v)
	}
	if versions == nil {
		versions = []domain.DocPageVersion{}
	}
	writeJSON(w, http.StatusOK, versions)
}
