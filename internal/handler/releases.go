package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/go-chi/chi/v5"
)

type releaseRequest struct {
	TagName      string `json:"tag_name"`
	Title        string `json:"title"`
	Body         string `json:"body"`
	IsDraft      bool   `json:"is_draft"`
	IsPrerelease bool   `json:"is_prerelease"`
}

func (h *Handler) ListReleases(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT rl.id, rl.repo_id, rl.tag_name, rl.title, rl.body, rl.is_draft, rl.is_prerelease,
		        rl.author_id, u.username, rl.created_at, rl.updated_at
		 FROM releases rl JOIN users u ON u.id = rl.author_id
		 WHERE rl.repo_id = $1 ORDER BY rl.created_at DESC`, repoID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list releases")
		return
	}
	defer rows.Close()

	var releases []domain.Release
	for rows.Next() {
		var rl domain.Release
		if err := rows.Scan(&rl.ID, &rl.RepoID, &rl.TagName, &rl.Title, &rl.Body,
			&rl.IsDraft, &rl.IsPrerelease, &rl.AuthorID, &rl.Author, &rl.CreatedAt, &rl.UpdatedAt); err != nil {
			continue
		}
		releases = append(releases, rl)
	}
	if releases == nil {
		releases = []domain.Release{}
	}
	writeJSON(w, http.StatusOK, releases)
}

func (h *Handler) GetRelease(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid release id")
		return
	}

	var rl domain.Release
	err = h.db.QueryRow(context.Background(),
		`SELECT rl.id, rl.repo_id, rl.tag_name, rl.title, rl.body, rl.is_draft, rl.is_prerelease,
		        rl.author_id, u.username, rl.created_at, rl.updated_at
		 FROM releases rl JOIN users u ON u.id = rl.author_id
		 WHERE rl.id = $1`, id,
	).Scan(&rl.ID, &rl.RepoID, &rl.TagName, &rl.Title, &rl.Body,
		&rl.IsDraft, &rl.IsPrerelease, &rl.AuthorID, &rl.Author, &rl.CreatedAt, &rl.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "release not found")
		return
	}
	writeJSON(w, http.StatusOK, rl)
}

func (h *Handler) CreateRelease(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	var req releaseRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.TagName == "" || req.Title == "" {
		writeError(w, http.StatusBadRequest, "tag_name and title are required")
		return
	}

	var id int64
	err = h.db.QueryRow(context.Background(),
		`INSERT INTO releases (repo_id, tag_name, title, body, is_draft, is_prerelease, author_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		repoID, req.TagName, req.Title, req.Body, req.IsDraft, req.IsPrerelease, claims.UserID,
	).Scan(&id)
	if err != nil {
		writeError(w, http.StatusConflict, "release with this tag already exists")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (h *Handler) UpdateRelease(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid release id")
		return
	}

	var req releaseRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	tag, err := h.db.Exec(context.Background(),
		`UPDATE releases SET title=$1, body=$2, is_draft=$3, is_prerelease=$4, updated_at=NOW() WHERE id=$5`,
		req.Title, req.Body, req.IsDraft, req.IsPrerelease, id)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "release not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) DeleteRelease(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid release id")
		return
	}

	tag, err := h.db.Exec(context.Background(), `DELETE FROM releases WHERE id = $1`, id)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "release not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
