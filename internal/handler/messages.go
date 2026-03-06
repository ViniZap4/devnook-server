package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/go-chi/chi/v5"
)

func (h *Handler) ListConversations(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	ctx := context.Background()

	rows, err := h.db.Query(ctx,
		`SELECT c.id, c.type, c.name, c.repo_owner, c.repo_name, c.org_name, c.issue_number,
		        c.created_at, c.updated_at
		 FROM conversations c
		 JOIN conversation_participants cp ON cp.conversation_id = c.id
		 WHERE cp.user_id = $1
		 ORDER BY c.updated_at DESC`, claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list conversations")
		return
	}
	defer rows.Close()

	convos := []domain.Conversation{}
	for rows.Next() {
		var c domain.Conversation
		if err := rows.Scan(&c.ID, &c.Type, &c.Name, &c.RepoOwner, &c.RepoName, &c.OrgName, &c.IssueNumber,
			&c.CreatedAt, &c.UpdatedAt); err != nil {
			continue
		}
		convos = append(convos, c)
	}

	// Load participants and last message for each conversation
	for i := range convos {
		convos[i].Participants = h.getConvoParticipants(ctx, convos[i].ID)
		convos[i].LastMessage = h.getLastMessage(ctx, convos[i].ID)
		convos[i].UnreadCount = h.getUnreadCount(ctx, convos[i].ID, claims.UserID)
	}

	writeJSON(w, http.StatusOK, convos)
}

func (h *Handler) GetConversation(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	ctx := context.Background()

	var c domain.Conversation
	err := h.db.QueryRow(ctx,
		`SELECT c.id, c.type, c.name, c.repo_owner, c.repo_name, c.org_name, c.issue_number,
		        c.created_at, c.updated_at
		 FROM conversations c
		 JOIN conversation_participants cp ON cp.conversation_id = c.id
		 WHERE c.id = $1 AND cp.user_id = $2`, id, claims.UserID,
	).Scan(&c.ID, &c.Type, &c.Name, &c.RepoOwner, &c.RepoName, &c.OrgName, &c.IssueNumber,
		&c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "conversation not found")
		return
	}

	c.Participants = h.getConvoParticipants(ctx, c.ID)
	c.LastMessage = h.getLastMessage(ctx, c.ID)
	c.UnreadCount = h.getUnreadCount(ctx, c.ID, claims.UserID)

	writeJSON(w, http.StatusOK, c)
}

func (h *Handler) CreateConversation(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	var req struct {
		Type         string   `json:"type"`
		Name         string   `json:"name"`
		Participants []string `json:"participants"`
		RepoOwner    *string  `json:"repo_owner"`
		RepoName     *string  `json:"repo_name"`
		OrgName      *string  `json:"org_name"`
		IssueNumber  *int     `json:"issue_number"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Type == "" {
		req.Type = "direct"
	}

	ctx := context.Background()

	// For direct conversations, check if one already exists between these users
	if req.Type == "direct" && len(req.Participants) == 1 {
		var targetID int64
		err := h.db.QueryRow(ctx, `SELECT id FROM users WHERE username = $1`, req.Participants[0]).Scan(&targetID)
		if err != nil {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}

		var existingID int64
		err = h.db.QueryRow(ctx,
			`SELECT c.id FROM conversations c
			 WHERE c.type = 'direct'
			 AND EXISTS(SELECT 1 FROM conversation_participants WHERE conversation_id = c.id AND user_id = $1)
			 AND EXISTS(SELECT 1 FROM conversation_participants WHERE conversation_id = c.id AND user_id = $2)`,
			claims.UserID, targetID).Scan(&existingID)
		if err == nil {
			writeJSON(w, http.StatusOK, map[string]int64{"id": existingID})
			return
		}
	}

	tx, err := h.db.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create conversation")
		return
	}
	defer tx.Rollback(ctx)

	var convoID int64
	err = tx.QueryRow(ctx,
		`INSERT INTO conversations (type, name, repo_owner, repo_name, org_name, issue_number)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		req.Type, req.Name, req.RepoOwner, req.RepoName, req.OrgName, req.IssueNumber,
	).Scan(&convoID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create conversation")
		return
	}

	// Add the creator as owner
	_, err = tx.Exec(ctx,
		`INSERT INTO conversation_participants (conversation_id, user_id, role) VALUES ($1, $2, 'owner')`,
		convoID, claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add creator")
		return
	}

	// Add other participants
	for _, username := range req.Participants {
		var uid int64
		err := h.db.QueryRow(ctx, `SELECT id FROM users WHERE username = $1`, username).Scan(&uid)
		if err != nil || uid == claims.UserID {
			continue
		}
		tx.Exec(ctx,
			`INSERT INTO conversation_participants (conversation_id, user_id, role) VALUES ($1, $2, 'member')`,
			convoID, uid)
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create conversation")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]int64{"id": convoID})
}

func (h *Handler) ListMessages(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	convoID, _ := strconv.ParseInt(chi.URLParam(r, "conversationId"), 10, 64)
	ctx := context.Background()

	// Verify the user is a participant
	var count int
	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM conversation_participants WHERE conversation_id=$1 AND user_id=$2`,
		convoID, claims.UserID).Scan(&count)
	if count == 0 {
		writeError(w, http.StatusForbidden, "not a participant")
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	beforeID, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)

	query := `SELECT m.id, m.conversation_id, m.sender_id, u.username, u.full_name,
			        m.content, m.type, m.reply_to_id, m.edited, m.created_at, m.updated_at
			 FROM chat_messages m
			 JOIN users u ON u.id = m.sender_id
			 WHERE m.conversation_id = $1`
	var args []any
	args = append(args, convoID)

	if beforeID > 0 {
		query += ` AND m.id < $2 ORDER BY m.created_at DESC LIMIT $3`
		args = append(args, beforeID, limit)
	} else {
		query += ` ORDER BY m.created_at DESC LIMIT $2`
		args = append(args, limit)
	}

	rows, err := h.db.Query(ctx, query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load messages")
		return
	}
	defer rows.Close()

	msgs := []domain.Message{}
	for rows.Next() {
		var m domain.Message
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.SenderUsername, &m.SenderFullName,
			&m.Content, &m.Type, &m.ReplyToID, &m.Edited, &m.CreatedAt, &m.UpdatedAt); err != nil {
			continue
		}
		m.Reactions = h.getMessageReactions(ctx, m.ID, claims.UserID)
		msgs = append(msgs, m)
	}

	// Reverse to chronological order
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}

	// Update last_read_at for the user
	h.db.Exec(ctx,
		`UPDATE conversation_participants SET last_read_at = NOW() WHERE conversation_id=$1 AND user_id=$2`,
		convoID, claims.UserID)

	writeJSON(w, http.StatusOK, msgs)
}

func (h *Handler) SendMessage(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	convoID, _ := strconv.ParseInt(chi.URLParam(r, "conversationId"), 10, 64)
	ctx := context.Background()

	var count int
	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM conversation_participants WHERE conversation_id=$1 AND user_id=$2`,
		convoID, claims.UserID).Scan(&count)
	if count == 0 {
		writeError(w, http.StatusForbidden, "not a participant")
		return
	}

	var req struct {
		Content   string `json:"content"`
		Type      string `json:"type"`
		ReplyToID *int64 `json:"reply_to_id"`
	}
	if err := readJSON(r, &req); err != nil || req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	if req.Type == "" {
		req.Type = "text"
	}

	var id int64
	err := h.db.QueryRow(ctx,
		`INSERT INTO chat_messages (conversation_id, sender_id, content, type, reply_to_id)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		convoID, claims.UserID, req.Content, req.Type, req.ReplyToID).Scan(&id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to send message")
		return
	}

	// Update conversation's updated_at
	h.db.Exec(ctx, `UPDATE conversations SET updated_at = NOW() WHERE id = $1`, convoID)

	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

func (h *Handler) EditMessage(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	msgID, _ := strconv.ParseInt(chi.URLParam(r, "messageId"), 10, 64)

	var req struct {
		Content string `json:"content"`
	}
	if err := readJSON(r, &req); err != nil || req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	tag, err := h.db.Exec(context.Background(),
		`UPDATE chat_messages SET content=$1, edited=true, updated_at=NOW() WHERE id=$2 AND sender_id=$3`,
		req.Content, msgID, claims.UserID)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "message not found or not yours")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) DeleteMessage(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	msgID, _ := strconv.ParseInt(chi.URLParam(r, "messageId"), 10, 64)

	h.db.Exec(context.Background(),
		`DELETE FROM chat_messages WHERE id=$1 AND sender_id=$2`, msgID, claims.UserID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ReactToMessage(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	msgID, _ := strconv.ParseInt(chi.URLParam(r, "messageId"), 10, 64)

	var req struct {
		Emoji string `json:"emoji"`
	}
	if err := readJSON(r, &req); err != nil || req.Emoji == "" {
		writeError(w, http.StatusBadRequest, "emoji is required")
		return
	}

	ctx := context.Background()
	// Toggle reaction: if exists remove, otherwise add
	tag, _ := h.db.Exec(ctx,
		`DELETE FROM message_reactions WHERE message_id=$1 AND user_id=$2 AND emoji=$3`,
		msgID, claims.UserID, req.Emoji)
	if tag.RowsAffected() == 0 {
		h.db.Exec(ctx,
			`INSERT INTO message_reactions (message_id, user_id, emoji) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
			msgID, claims.UserID, req.Emoji)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) UnreadMessageCount(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)

	var total int
	h.db.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(cnt), 0) FROM (
			SELECT COUNT(*) AS cnt
			FROM chat_messages m
			JOIN conversation_participants cp ON cp.conversation_id = m.conversation_id AND cp.user_id = $1
			WHERE m.created_at > cp.last_read_at AND m.sender_id != $1
		) sub`, claims.UserID).Scan(&total)

	writeJSON(w, http.StatusOK, map[string]int{"count": total})
}

// ── Helpers ─────────────────────────────────────────────────────────

func (h *Handler) getConvoParticipants(ctx context.Context, convoID int64) []domain.ConversationParticipant {
	rows, err := h.db.Query(ctx,
		`SELECT u.id, u.username, u.full_name, u.avatar_url, cp.role
		 FROM conversation_participants cp
		 JOIN users u ON u.id = cp.user_id
		 WHERE cp.conversation_id = $1`, convoID)
	if err != nil {
		return []domain.ConversationParticipant{}
	}
	defer rows.Close()

	var participants []domain.ConversationParticipant
	for rows.Next() {
		var p domain.ConversationParticipant
		if err := rows.Scan(&p.UserID, &p.Username, &p.FullName, &p.AvatarURL, &p.Role); err != nil {
			continue
		}
		participants = append(participants, p)
	}
	if participants == nil {
		return []domain.ConversationParticipant{}
	}
	return participants
}

func (h *Handler) getLastMessage(ctx context.Context, convoID int64) *domain.Message {
	var m domain.Message
	err := h.db.QueryRow(ctx,
		`SELECT m.id, m.conversation_id, m.sender_id, u.username, u.full_name,
		        m.content, m.type, m.reply_to_id, m.edited, m.created_at, m.updated_at
		 FROM chat_messages m
		 JOIN users u ON u.id = m.sender_id
		 WHERE m.conversation_id = $1
		 ORDER BY m.created_at DESC LIMIT 1`, convoID,
	).Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.SenderUsername, &m.SenderFullName,
		&m.Content, &m.Type, &m.ReplyToID, &m.Edited, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return nil
	}
	m.Reactions = []domain.MessageReaction{}
	return &m
}

func (h *Handler) getUnreadCount(ctx context.Context, convoID, userID int64) int {
	var count int
	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM chat_messages m
		 JOIN conversation_participants cp ON cp.conversation_id = m.conversation_id AND cp.user_id = $2
		 WHERE m.conversation_id = $1 AND m.created_at > cp.last_read_at AND m.sender_id != $2`,
		convoID, userID).Scan(&count)
	return count
}

func (h *Handler) getMessageReactions(ctx context.Context, msgID, userID int64) []domain.MessageReaction {
	rows, err := h.db.Query(ctx,
		`SELECT emoji, COUNT(*) AS cnt,
		        bool_or(user_id = $2) AS reacted
		 FROM message_reactions
		 WHERE message_id = $1
		 GROUP BY emoji
		 ORDER BY MIN(created_at)`, msgID, userID)
	if err != nil {
		return []domain.MessageReaction{}
	}
	defer rows.Close()

	var reactions []domain.MessageReaction
	for rows.Next() {
		var r domain.MessageReaction
		if err := rows.Scan(&r.Emoji, &r.Count, &r.Reacted); err != nil {
			continue
		}
		reactions = append(reactions, r)
	}
	if reactions == nil {
		return []domain.MessageReaction{}
	}
	return reactions
}
