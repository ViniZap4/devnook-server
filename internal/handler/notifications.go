package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/ViniZap4/devnook-server/internal/ws"
	"github.com/go-chi/chi/v5"
)

func (h *Handler) ListNotifications(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)

	unreadOnly := r.URL.Query().Get("unread") == "true"

	var query string
	var args []any
	if unreadOnly {
		query = `SELECT id, user_id, repo_id, type, title, body, read, link, created_at
		         FROM notifications WHERE user_id = $1 AND read = false ORDER BY created_at DESC LIMIT 50`
		args = []any{claims.UserID}
	} else {
		query = `SELECT id, user_id, repo_id, type, title, body, read, link, created_at
		         FROM notifications WHERE user_id = $1 ORDER BY created_at DESC LIMIT 50`
		args = []any{claims.UserID}
	}

	rows, err := h.db.Query(context.Background(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list notifications")
		return
	}
	defer rows.Close()

	var notifications []domain.Notification
	for rows.Next() {
		var n domain.Notification
		if err := rows.Scan(&n.ID, &n.UserID, &n.RepoID, &n.Type, &n.Title, &n.Body,
			&n.Read, &n.Link, &n.CreatedAt); err != nil {
			continue
		}
		notifications = append(notifications, n)
	}
	if notifications == nil {
		notifications = []domain.Notification{}
	}
	writeJSON(w, http.StatusOK, notifications)
}

func (h *Handler) UnreadNotificationCount(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)

	var count int
	h.db.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND read = false`,
		claims.UserID).Scan(&count)

	writeJSON(w, http.StatusOK, map[string]int{"count": count})
}

func (h *Handler) MarkNotificationRead(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid notification id")
		return
	}

	h.db.Exec(context.Background(),
		`UPDATE notifications SET read = true WHERE id = $1 AND user_id = $2`,
		id, claims.UserID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) MarkAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)

	h.db.Exec(context.Background(),
		`UPDATE notifications SET read = true WHERE user_id = $1 AND read = false`,
		claims.UserID)
	w.WriteHeader(http.StatusNoContent)
}

// CreateNotification inserts a notification and pushes it via WebSocket.
func (h *Handler) CreateNotification(userID int64, repoID *int64, nType, title, body, link string) {
	ctx := context.Background()
	var n domain.Notification
	err := h.db.QueryRow(ctx,
		`INSERT INTO notifications (user_id, repo_id, type, title, body, link)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, user_id, repo_id, type, title, body, read, link, created_at`,
		userID, repoID, nType, title, body, link,
	).Scan(&n.ID, &n.UserID, &n.RepoID, &n.Type, &n.Title, &n.Body, &n.Read, &n.Link, &n.CreatedAt)
	if err != nil {
		return
	}

	h.hub.SendToUser(userID, ws.Event{Type: "notification", Data: n})

	// Also send updated unread count
	var count int
	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND read = false`, userID).Scan(&count)
	h.hub.SendToUser(userID, ws.Event{Type: "notification_count", Data: map[string]int{"count": count}})
}
