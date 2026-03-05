package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/go-chi/chi/v5"
)

type webhookRequest struct {
	URL    string   `json:"url"`
	Secret string   `json:"secret"`
	Events []string `json:"events"`
	Active *bool    `json:"active,omitempty"`
}

func (h *Handler) ListWebhooks(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT id, repo_id, url, secret, events, active, created_at, updated_at
		 FROM webhooks WHERE repo_id = $1 ORDER BY created_at DESC`, repoID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list webhooks")
		return
	}
	defer rows.Close()

	var hooks []domain.Webhook
	for rows.Next() {
		var wh domain.Webhook
		if err := rows.Scan(&wh.ID, &wh.RepoID, &wh.URL, &wh.Secret, &wh.Events,
			&wh.Active, &wh.CreatedAt, &wh.UpdatedAt); err != nil {
			continue
		}
		hooks = append(hooks, wh)
	}
	if hooks == nil {
		hooks = []domain.Webhook{}
	}
	writeJSON(w, http.StatusOK, hooks)
}

func (h *Handler) CreateWebhook(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	var req webhookRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.URL == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	if req.Events == nil {
		req.Events = []string{"push"}
	}

	var id int64
	err = h.db.QueryRow(context.Background(),
		`INSERT INTO webhooks (repo_id, url, secret, events) VALUES ($1, $2, $3, $4) RETURNING id`,
		repoID, req.URL, req.Secret, req.Events,
	).Scan(&id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create webhook")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (h *Handler) UpdateWebhook(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid webhook id")
		return
	}

	var req webhookRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx := context.Background()
	if req.URL != "" {
		h.db.Exec(ctx, `UPDATE webhooks SET url=$1, updated_at=NOW() WHERE id=$2`, req.URL, id)
	}
	if req.Events != nil {
		h.db.Exec(ctx, `UPDATE webhooks SET events=$1, updated_at=NOW() WHERE id=$2`, req.Events, id)
	}
	if req.Active != nil {
		h.db.Exec(ctx, `UPDATE webhooks SET active=$1, updated_at=NOW() WHERE id=$2`, *req.Active, id)
	}
	if req.Secret != "" {
		h.db.Exec(ctx, `UPDATE webhooks SET secret=$1, updated_at=NOW() WHERE id=$2`, req.Secret, id)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) DeleteWebhook(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid webhook id")
		return
	}

	tag, err := h.db.Exec(context.Background(), `DELETE FROM webhooks WHERE id = $1`, id)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "webhook not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
