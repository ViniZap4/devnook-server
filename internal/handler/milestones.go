package handler

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/go-chi/chi/v5"
)

type milestoneRequest struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	State       *string `json:"state,omitempty"`
	DueDate     *string `json:"due_date,omitempty"`
}

func (h *Handler) ListMilestones(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	state := r.URL.Query().Get("state")
	if state == "" {
		state = "open"
	}

	var query string
	var args []any
	if state == "all" {
		query = `SELECT id, repo_id, title, description, state, due_date, created_at, updated_at FROM milestones WHERE repo_id = $1 ORDER BY created_at DESC`
		args = []any{repoID}
	} else {
		query = `SELECT id, repo_id, title, description, state, due_date, created_at, updated_at FROM milestones WHERE repo_id = $1 AND state = $2 ORDER BY created_at DESC`
		args = []any{repoID, state}
	}

	rows, err := h.db.Query(context.Background(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list milestones")
		return
	}
	defer rows.Close()

	var milestones []domain.Milestone
	for rows.Next() {
		var m domain.Milestone
		if err := rows.Scan(&m.ID, &m.RepoID, &m.Title, &m.Description, &m.State, &m.DueDate, &m.CreatedAt, &m.UpdatedAt); err != nil {
			continue
		}

		// Count open/closed issues
		h.db.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM issues WHERE milestone_id = $1 AND state = 'open'`, m.ID,
		).Scan(&m.OpenIssues)
		h.db.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM issues WHERE milestone_id = $1 AND state = 'closed'`, m.ID,
		).Scan(&m.ClosedIssues)

		milestones = append(milestones, m)
	}
	if milestones == nil {
		milestones = []domain.Milestone{}
	}
	writeJSON(w, http.StatusOK, milestones)
}

func (h *Handler) CreateMilestone(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")

	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	var req milestoneRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	var dueDate *time.Time
	if req.DueDate != nil && *req.DueDate != "" {
		t, err := time.Parse(time.RFC3339, *req.DueDate)
		if err != nil {
			t, err = time.Parse("2006-01-02", *req.DueDate)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid due_date format")
				return
			}
		}
		dueDate = &t
	}

	var id int64
	err = h.db.QueryRow(context.Background(),
		`INSERT INTO milestones (repo_id, title, description, due_date) VALUES ($1, $2, $3, $4) RETURNING id`,
		repoID, req.Title, req.Description, dueDate,
	).Scan(&id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create milestone")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (h *Handler) UpdateMilestone(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid milestone id")
		return
	}

	var req milestoneRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx := context.Background()
	sets := []string{}
	args := []any{}
	argN := 1

	if req.Title != "" {
		sets = append(sets, fmt.Sprintf("title=$%d", argN))
		args = append(args, req.Title)
		argN++
	}
	if req.Description != "" {
		sets = append(sets, fmt.Sprintf("description=$%d", argN))
		args = append(args, req.Description)
		argN++
	}
	if req.State != nil {
		sets = append(sets, fmt.Sprintf("state=$%d", argN))
		args = append(args, *req.State)
		argN++
	}
	if req.DueDate != nil {
		if *req.DueDate == "" {
			sets = append(sets, "due_date=NULL")
		} else {
			t, err := time.Parse(time.RFC3339, *req.DueDate)
			if err != nil {
				t, _ = time.Parse("2006-01-02", *req.DueDate)
			}
			sets = append(sets, fmt.Sprintf("due_date=$%d", argN))
			args = append(args, t)
			argN++
		}
	}

	if len(sets) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	sets = append(sets, "updated_at=NOW()")
	query := fmt.Sprintf("UPDATE milestones SET %s WHERE id=$%d",
		strings.Join(sets, ", "), argN)
	args = append(args, id)

	if _, err := h.db.Exec(ctx, query, args...); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update milestone")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) DeleteMilestone(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid milestone id")
		return
	}

	tag, err := h.db.Exec(context.Background(), `DELETE FROM milestones WHERE id = $1`, id)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "milestone not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
