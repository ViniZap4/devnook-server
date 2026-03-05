package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/go-chi/chi/v5"
)

type labelRequest struct {
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

func (h *Handler) ListLabels(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT id, repo_id, name, color, description FROM labels WHERE repo_id = $1 ORDER BY name`, repoID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list labels")
		return
	}
	defer rows.Close()

	var labels []domain.Label
	for rows.Next() {
		var l domain.Label
		if err := rows.Scan(&l.ID, &l.RepoID, &l.Name, &l.Color, &l.Description); err != nil {
			continue
		}
		labels = append(labels, l)
	}
	if labels == nil {
		labels = []domain.Label{}
	}
	writeJSON(w, http.StatusOK, labels)
}

func (h *Handler) CreateLabel(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	var req labelRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Color == "" {
		req.Color = "#cccccc"
	}

	var id int64
	err = h.db.QueryRow(context.Background(),
		`INSERT INTO labels (repo_id, name, color, description) VALUES ($1, $2, $3, $4) RETURNING id`,
		repoID, req.Name, req.Color, req.Description,
	).Scan(&id)
	if err != nil {
		writeError(w, http.StatusConflict, "label already exists")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (h *Handler) UpdateLabel(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid label id")
		return
	}

	var req labelRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	tag, err := h.db.Exec(context.Background(),
		`UPDATE labels SET name = $1, color = $2, description = $3 WHERE id = $4`,
		req.Name, req.Color, req.Description, id)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "label not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) DeleteLabel(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid label id")
		return
	}

	tag, err := h.db.Exec(context.Background(), `DELETE FROM labels WHERE id = $1`, id)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "label not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) AddIssueLabel(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	var req struct {
		LabelID int64 `json:"label_id"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var issueID int64
	err = h.db.QueryRow(context.Background(),
		`SELECT id FROM issues WHERE repo_id = $1 AND number = $2`, repoID, number,
	).Scan(&issueID)
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}

	_, err = h.db.Exec(context.Background(),
		`INSERT INTO issue_labels (issue_id, label_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		issueID, req.LabelID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to add label")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RemoveIssueLabel(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}
	labelID, err := strconv.ParseInt(chi.URLParam(r, "labelId"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid label id")
		return
	}

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	var issueID int64
	err = h.db.QueryRow(context.Background(),
		`SELECT id FROM issues WHERE repo_id = $1 AND number = $2`, repoID, number,
	).Scan(&issueID)
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}

	h.db.Exec(context.Background(),
		`DELETE FROM issue_labels WHERE issue_id = $1 AND label_id = $2`, issueID, labelID)
	w.WriteHeader(http.StatusNoContent)
}
