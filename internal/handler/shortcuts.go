package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/go-chi/chi/v5"
)

func (h *Handler) ListShortcuts(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	rows, err := h.db.Query(context.Background(),
		`SELECT id, user_id, title, url, icon_url, color, created_at, updated_at
		 FROM shortcuts WHERE user_id = $1 ORDER BY created_at`, claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list shortcuts")
		return
	}
	defer rows.Close()

	var shortcuts []domain.Shortcut
	for rows.Next() {
		var s domain.Shortcut
		if err := rows.Scan(&s.ID, &s.UserID, &s.Title, &s.URL, &s.IconURL, &s.Color, &s.CreatedAt, &s.UpdatedAt); err != nil {
			continue
		}
		shortcuts = append(shortcuts, s)
	}
	if shortcuts == nil {
		shortcuts = []domain.Shortcut{}
	}
	writeJSON(w, http.StatusOK, shortcuts)
}

type shortcutRequest struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	IconURL string `json:"icon_url"`
	Color   string `json:"color"`
}

func (h *Handler) CreateShortcut(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	var req shortcutRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" || req.URL == "" {
		writeError(w, http.StatusBadRequest, "title and url are required")
		return
	}

	var id int64
	err := h.db.QueryRow(context.Background(),
		`INSERT INTO shortcuts (user_id, title, url, icon_url, color)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		claims.UserID, req.Title, req.URL, req.IconURL, req.Color,
	).Scan(&id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create shortcut")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id})
}

func (h *Handler) UpdateShortcut(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid shortcut id")
		return
	}

	var req shortcutRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	tag, err := h.db.Exec(context.Background(),
		`UPDATE shortcuts SET title=$1, url=$2, icon_url=$3, color=$4, updated_at=NOW()
		 WHERE id=$5 AND user_id=$6`,
		req.Title, req.URL, req.IconURL, req.Color, id, claims.UserID)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "shortcut not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) DeleteShortcut(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid shortcut id")
		return
	}

	tag, err := h.db.Exec(context.Background(),
		`DELETE FROM shortcuts WHERE id=$1 AND user_id=$2`, id, claims.UserID)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "shortcut not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
