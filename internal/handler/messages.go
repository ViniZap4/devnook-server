package handler

import (
	"context"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/ViniZap4/devnook-server/internal/ws"
	"github.com/gofiber/fiber/v2"
)

func (h *Handler) ListConversations(c *fiber.Ctx) error {
	claims := getClaims(c)
	ctx := context.Background()

	rows, err := h.db.Query(ctx,
		`SELECT c.id, c.type, c.name, c.repo_owner, c.repo_name, c.org_name, c.issue_number,
		        c.created_at, c.updated_at
		 FROM conversations c
		 JOIN conversation_participants cp ON cp.conversation_id = c.id
		 WHERE cp.user_id = $1
		 ORDER BY c.updated_at DESC`, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to list conversations")
	}
	defer rows.Close()

	convos := []domain.Conversation{}
	for rows.Next() {
		var cv domain.Conversation
		if err := rows.Scan(&cv.ID, &cv.Type, &cv.Name, &cv.RepoOwner, &cv.RepoName, &cv.OrgName, &cv.IssueNumber,
			&cv.CreatedAt, &cv.UpdatedAt); err != nil {
			continue
		}
		convos = append(convos, cv)
	}

	// Load participants and last message for each conversation
	for i := range convos {
		convos[i].Participants = h.getConvoParticipants(ctx, convos[i].ID)
		convos[i].LastMessage = h.getLastMessage(ctx, convos[i].ID)
		convos[i].UnreadCount = h.getUnreadCount(ctx, convos[i].ID, claims.UserID)
	}

	return writeJSON(c, fiber.StatusOK, convos)
}

func (h *Handler) GetConversation(c *fiber.Ctx) error {
	claims := getClaims(c)
	id, _ := strconv.ParseInt(c.Params("id"), 10, 64)
	ctx := context.Background()

	var cv domain.Conversation
	err := h.db.QueryRow(ctx,
		`SELECT c.id, c.type, c.name, c.repo_owner, c.repo_name, c.org_name, c.issue_number,
		        c.created_at, c.updated_at
		 FROM conversations c
		 JOIN conversation_participants cp ON cp.conversation_id = c.id
		 WHERE c.id = $1 AND cp.user_id = $2`, id, claims.UserID,
	).Scan(&cv.ID, &cv.Type, &cv.Name, &cv.RepoOwner, &cv.RepoName, &cv.OrgName, &cv.IssueNumber,
		&cv.CreatedAt, &cv.UpdatedAt)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "conversation not found")
	}

	cv.Participants = h.getConvoParticipants(ctx, cv.ID)
	cv.LastMessage = h.getLastMessage(ctx, cv.ID)
	cv.UnreadCount = h.getUnreadCount(ctx, cv.ID, claims.UserID)

	return writeJSON(c, fiber.StatusOK, cv)
}

func (h *Handler) CreateConversation(c *fiber.Ctx) error {
	claims := getClaims(c)
	var req struct {
		Type         string   `json:"type"`
		Name         string   `json:"name"`
		Participants []string `json:"participants"`
		RepoOwner    *string  `json:"repo_owner"`
		RepoName     *string  `json:"repo_name"`
		OrgName      *string  `json:"org_name"`
		IssueNumber  *int     `json:"issue_number"`
	}
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Type == "" {
		req.Type = "direct"
	}
	validTypes := map[string]bool{"direct": true, "group": true, "repo": true, "org": true, "issue": true}
	if !validTypes[req.Type] {
		return writeError(c, fiber.StatusBadRequest, "invalid conversation type")
	}

	ctx := context.Background()

	// For direct conversations, check if one already exists between these users
	if req.Type == "direct" && len(req.Participants) == 1 {
		var targetID int64
		err := h.db.QueryRow(ctx, `SELECT id FROM users WHERE username = $1`, req.Participants[0]).Scan(&targetID)
		if err != nil {
			return writeError(c, fiber.StatusNotFound, "user not found")
		}

		var existingID int64
		err = h.db.QueryRow(ctx,
			`SELECT c.id FROM conversations c
			 WHERE c.type = 'direct'
			 AND EXISTS(SELECT 1 FROM conversation_participants WHERE conversation_id = c.id AND user_id = $1)
			 AND EXISTS(SELECT 1 FROM conversation_participants WHERE conversation_id = c.id AND user_id = $2)`,
			claims.UserID, targetID).Scan(&existingID)
		if err == nil {
			return writeJSON(c, fiber.StatusOK, map[string]int64{"id": existingID})
		}
	}

	tx, err := h.db.Begin(ctx)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to create conversation")
	}
	defer tx.Rollback(ctx)

	var convoID int64
	err = tx.QueryRow(ctx,
		`INSERT INTO conversations (type, name, repo_owner, repo_name, org_name, issue_number)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		req.Type, req.Name, req.RepoOwner, req.RepoName, req.OrgName, req.IssueNumber,
	).Scan(&convoID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to create conversation")
	}

	// Add the creator as owner
	_, err = tx.Exec(ctx,
		`INSERT INTO conversation_participants (conversation_id, user_id, role) VALUES ($1, $2, 'owner')`,
		convoID, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to add creator")
	}

	// Add other participants
	for _, username := range req.Participants {
		var uid int64
		err := tx.QueryRow(ctx, `SELECT id FROM users WHERE username = $1`, username).Scan(&uid)
		if err != nil || uid == claims.UserID {
			continue
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO conversation_participants (conversation_id, user_id, role) VALUES ($1, $2, 'member')`,
			convoID, uid); err != nil {
			log.Printf("failed to add participant %s: %v", username, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to create conversation")
	}

	return writeJSON(c, fiber.StatusCreated, map[string]int64{"id": convoID})
}

func (h *Handler) ListMessages(c *fiber.Ctx) error {
	claims := getClaims(c)
	convoID, _ := strconv.ParseInt(c.Params("conversationId"), 10, 64)
	ctx := context.Background()

	// Verify the user is a participant
	var count int
	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM conversation_participants WHERE conversation_id=$1 AND user_id=$2`,
		convoID, claims.UserID).Scan(&count)
	if count == 0 {
		return writeError(c, fiber.StatusForbidden, "not a participant")
	}

	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	beforeID, _ := strconv.ParseInt(c.Query("before"), 10, 64)

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
		return writeError(c, fiber.StatusInternalServerError, "failed to load messages")
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
	if _, err := h.db.Exec(ctx,
		`UPDATE conversation_participants SET last_read_at = NOW() WHERE conversation_id=$1 AND user_id=$2`,
		convoID, claims.UserID); err != nil {
		log.Printf("failed to update last_read_at for user %d in convo %d: %v", claims.UserID, convoID, err)
	}

	return writeJSON(c, fiber.StatusOK, msgs)
}

func (h *Handler) SendMessage(c *fiber.Ctx) error {
	claims := getClaims(c)
	convoID, _ := strconv.ParseInt(c.Params("conversationId"), 10, 64)
	ctx := context.Background()

	var count int
	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM conversation_participants WHERE conversation_id=$1 AND user_id=$2`,
		convoID, claims.UserID).Scan(&count)
	if count == 0 {
		return writeError(c, fiber.StatusForbidden, "not a participant")
	}

	var req struct {
		Content   string `json:"content"`
		Type      string `json:"type"`
		ReplyToID *int64 `json:"reply_to_id"`
	}
	if err := readJSON(c, &req); err != nil || req.Content == "" {
		return writeError(c, fiber.StatusBadRequest, "content is required")
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
		return writeError(c, fiber.StatusInternalServerError, "failed to send message")
	}

	// Update conversation's updated_at
	h.db.Exec(ctx, `UPDATE conversations SET updated_at = NOW() WHERE id = $1`, convoID)

	// Broadcast via WebSocket to all participants
	go h.broadcastChatMessage(convoID, id, claims.UserID, claims.Username, req.Content, req.Type, req.ReplyToID)

	return writeJSON(c, fiber.StatusCreated, map[string]int64{"id": id})
}

func (h *Handler) EditMessage(c *fiber.Ctx) error {
	claims := getClaims(c)
	convoID, _ := strconv.ParseInt(c.Params("conversationId"), 10, 64)
	msgID, _ := strconv.ParseInt(c.Params("messageId"), 10, 64)
	ctx := context.Background()

	// Verify the user is a participant
	var participantCount int
	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM conversation_participants WHERE conversation_id=$1 AND user_id=$2`,
		convoID, claims.UserID).Scan(&participantCount)
	if participantCount == 0 {
		return writeError(c, fiber.StatusForbidden, "not a participant")
	}

	var req struct {
		Content string `json:"content"`
	}
	if err := readJSON(c, &req); err != nil || req.Content == "" {
		return writeError(c, fiber.StatusBadRequest, "content is required")
	}

	tag, err := h.db.Exec(ctx,
		`UPDATE chat_messages SET content=$1, edited=true, updated_at=NOW() WHERE id=$2 AND sender_id=$3 AND conversation_id=$4`,
		req.Content, msgID, claims.UserID, convoID)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "message not found or not yours")
	}
	go h.broadcastChatEdit(convoID, msgID, claims.UserID, req.Content)
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) DeleteMessage(c *fiber.Ctx) error {
	claims := getClaims(c)
	convoID, _ := strconv.ParseInt(c.Params("conversationId"), 10, 64)
	msgID, _ := strconv.ParseInt(c.Params("messageId"), 10, 64)
	ctx := context.Background()

	// Verify the user is a participant
	var participantCount int
	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM conversation_participants WHERE conversation_id=$1 AND user_id=$2`,
		convoID, claims.UserID).Scan(&participantCount)
	if participantCount == 0 {
		return writeError(c, fiber.StatusForbidden, "not a participant")
	}

	tag, err := h.db.Exec(ctx,
		`DELETE FROM chat_messages WHERE id=$1 AND sender_id=$2 AND conversation_id=$3`, msgID, claims.UserID, convoID)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "message not found or not yours")
	}
	go h.broadcastChatDelete(convoID, msgID, claims.UserID)
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) ReactToMessage(c *fiber.Ctx) error {
	claims := getClaims(c)
	convoID, _ := strconv.ParseInt(c.Params("conversationId"), 10, 64)
	msgID, _ := strconv.ParseInt(c.Params("messageId"), 10, 64)

	var req struct {
		Emoji string `json:"emoji"`
	}
	if err := readJSON(c, &req); err != nil || req.Emoji == "" {
		return writeError(c, fiber.StatusBadRequest, "emoji is required")
	}

	ctx := context.Background()

	// Verify the user is a participant
	var participantCount int
	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM conversation_participants WHERE conversation_id=$1 AND user_id=$2`,
		convoID, claims.UserID).Scan(&participantCount)
	if participantCount == 0 {
		return writeError(c, fiber.StatusForbidden, "not a participant")
	}

	// Toggle reaction: if exists remove, otherwise add
	tag, _ := h.db.Exec(ctx,
		`DELETE FROM message_reactions WHERE message_id=$1 AND user_id=$2 AND emoji=$3`,
		msgID, claims.UserID, req.Emoji)
	added := tag.RowsAffected() == 0
	if added {
		h.db.Exec(ctx,
			`INSERT INTO message_reactions (message_id, user_id, emoji) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
			msgID, claims.UserID, req.Emoji)
	}
	var username string
	h.db.QueryRow(ctx, `SELECT username FROM users WHERE id = $1`, claims.UserID).Scan(&username)
	go h.broadcastChatReact(convoID, msgID, claims.UserID, username, req.Emoji, added)
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) UnreadMessageCount(c *fiber.Ctx) error {
	claims := getClaims(c)

	var total int
	h.db.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(cnt), 0) FROM (
			SELECT COUNT(*) AS cnt
			FROM chat_messages m
			JOIN conversation_participants cp ON cp.conversation_id = m.conversation_id AND cp.user_id = $1
			WHERE m.created_at > cp.last_read_at AND m.sender_id != $1
		) sub`, claims.UserID).Scan(&total)

	return writeJSON(c, fiber.StatusOK, map[string]int{"count": total})
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

func (h *Handler) broadcastChatMessage(convoID, msgID, senderID int64, senderUsername, content, msgType string, replyToID *int64) {
	ctx := context.Background()

	// Get sender full name
	var senderFullName string
	h.db.QueryRow(ctx, `SELECT full_name FROM users WHERE id = $1`, senderID).Scan(&senderFullName)

	// Fetch actual created_at from the DB instead of using time.Now()
	var createdAt time.Time
	h.db.QueryRow(ctx, `SELECT created_at FROM chat_messages WHERE id = $1`, msgID).Scan(&createdAt)

	// Get all participant user IDs
	rows, err := h.db.Query(ctx,
		`SELECT user_id FROM conversation_participants WHERE conversation_id = $1`, convoID)
	if err != nil {
		return
	}
	defer rows.Close()

	var participantIDs []int64
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err == nil {
			participantIDs = append(participantIDs, uid)
		}
	}

	msg := domain.Message{
		ID:             msgID,
		ConversationID: convoID,
		SenderID:       senderID,
		SenderUsername: senderUsername,
		SenderFullName: senderFullName,
		Content:        content,
		Type:           msgType,
		ReplyToID:      replyToID,
		Reactions:      []domain.MessageReaction{},
		CreatedAt:      createdAt,
		UpdatedAt:      createdAt,
	}

	h.hub.SendToUsers(participantIDs, ws.Event{
		Type: "chat_message",
		Data: msg,
	})

	// Send message_unread event to non-sender participants
	for _, uid := range participantIDs {
		if uid == senderID {
			continue
		}
		unread := h.getUnreadCount(ctx, convoID, uid)
		h.hub.SendToUser(uid, ws.Event{
			Type: "message_unread",
			Data: map[string]any{
				"conversation_id": convoID,
				"count":           unread,
			},
		})
	}
}

func (h *Handler) broadcastChatEdit(convoID, msgID, senderID int64, newContent string) {
	ctx := context.Background()

	// Fetch actual updated_at from the DB instead of using time.Now()
	var updatedAt time.Time
	if err := h.db.QueryRow(ctx, `SELECT updated_at FROM chat_messages WHERE id=$1`, msgID).Scan(&updatedAt); err != nil {
		updatedAt = time.Now()
	}

	rows, err := h.db.Query(ctx,
		`SELECT user_id FROM conversation_participants WHERE conversation_id = $1`, convoID)
	if err != nil {
		return
	}
	defer rows.Close()

	var participantIDs []int64
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err == nil {
			participantIDs = append(participantIDs, uid)
		}
	}

	h.hub.SendToUsers(participantIDs, ws.Event{
		Type: "chat_message_edit",
		Data: map[string]any{
			"id":              msgID,
			"conversation_id": convoID,
			"content":         newContent,
			"sender_id":       senderID,
			"edited":          true,
			"updated_at":      updatedAt,
		},
	})
}

func (h *Handler) broadcastChatDelete(convoID, msgID, senderID int64) {
	ctx := context.Background()
	rows, err := h.db.Query(ctx,
		`SELECT user_id FROM conversation_participants WHERE conversation_id = $1`, convoID)
	if err != nil {
		return
	}
	defer rows.Close()

	var participantIDs []int64
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err == nil {
			participantIDs = append(participantIDs, uid)
		}
	}

	h.hub.SendToUsers(participantIDs, ws.Event{
		Type: "chat_message_delete",
		Data: map[string]any{
			"id":              msgID,
			"conversation_id": convoID,
			"sender_id":       senderID,
		},
	})
}

func (h *Handler) broadcastChatReact(convoID, msgID, userID int64, username, emoji string, added bool) {
	ctx := context.Background()
	rows, err := h.db.Query(ctx,
		`SELECT user_id FROM conversation_participants WHERE conversation_id = $1`, convoID)
	if err != nil {
		return
	}
	defer rows.Close()

	var participantIDs []int64
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err == nil {
			participantIDs = append(participantIDs, uid)
		}
	}

	h.hub.SendToUsers(participantIDs, ws.Event{
		Type: "chat_message_react",
		Data: map[string]any{
			"message_id":      msgID,
			"conversation_id": convoID,
			"user_id":         userID,
			"username":        username,
			"emoji":           emoji,
			"added":           added,
		},
	})
}

func (h *Handler) InitiateCall(c *fiber.Ctx) error {
	claims := getClaims(c)
	convoID, _ := strconv.ParseInt(c.Params("conversationId"), 10, 64)
	ctx := context.Background()

	// Verify the user is a participant
	var count int
	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM conversation_participants WHERE conversation_id=$1 AND user_id=$2`,
		convoID, claims.UserID).Scan(&count)
	if count == 0 {
		return writeError(c, fiber.StatusForbidden, "not a participant")
	}

	// Get all participant IDs except the caller
	rows, err := h.db.Query(ctx,
		`SELECT user_id FROM conversation_participants WHERE conversation_id = $1 AND user_id != $2`,
		convoID, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to get participants")
	}
	defer rows.Close()

	var participantIDs []int64
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err == nil {
			participantIDs = append(participantIDs, uid)
		}
	}

	// Broadcast call_initiate event to other participants
	h.hub.SendToUsers(participantIDs, ws.Event{
		Type: "call_initiate",
		Data: map[string]any{
			"conversation_id": convoID,
			"caller_id":       claims.UserID,
			"caller_username": claims.Username,
		},
	})

	return writeJSON(c, fiber.StatusOK, map[string]string{"status": "initiated"})
}

func (h *Handler) TypingIndicator(c *fiber.Ctx) error {
	claims := getClaims(c)
	convoID, _ := strconv.ParseInt(c.Params("conversationId"), 10, 64)
	ctx := context.Background()

	// Verify the user is a participant
	var count int
	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM conversation_participants WHERE conversation_id=$1 AND user_id=$2`,
		convoID, claims.UserID).Scan(&count)
	if count == 0 {
		return writeError(c, fiber.StatusForbidden, "not a participant")
	}

	// Get all participant IDs except the sender
	rows, err := h.db.Query(ctx,
		`SELECT user_id FROM conversation_participants WHERE conversation_id = $1 AND user_id != $2`,
		convoID, claims.UserID)
	if err != nil {
		return c.SendStatus(fiber.StatusNoContent)
	}
	defer rows.Close()

	var participantIDs []int64
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err == nil {
			participantIDs = append(participantIDs, uid)
		}
	}

	h.hub.SendToUsers(participantIDs, ws.Event{
		Type: "chat_typing",
		Data: map[string]any{
			"conversation_id": convoID,
			"user_id":         claims.UserID,
			"username":        claims.Username,
		},
	})

	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) MarkConversationRead(c *fiber.Ctx) error {
	claims := getClaims(c)
	convoID, _ := strconv.ParseInt(c.Params("conversationId"), 10, 64)
	ctx := context.Background()

	tag, err := h.db.Exec(ctx,
		`UPDATE conversation_participants SET last_read_at = NOW() WHERE conversation_id=$1 AND user_id=$2`,
		convoID, claims.UserID)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "not a participant")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) AddParticipant(c *fiber.Ctx) error {
	claims := getClaims(c)
	convoID, _ := strconv.ParseInt(c.Params("conversationId"), 10, 64)
	ctx := context.Background()

	// Verify the requester is a participant
	var count int
	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM conversation_participants WHERE conversation_id=$1 AND user_id=$2`,
		convoID, claims.UserID).Scan(&count)
	if count == 0 {
		return writeError(c, fiber.StatusForbidden, "not a participant")
	}

	// Prevent adding participants to direct conversations
	var convoType string
	h.db.QueryRow(ctx, `SELECT type FROM conversations WHERE id = $1`, convoID).Scan(&convoType)
	if convoType == "direct" {
		return writeError(c, fiber.StatusBadRequest, "cannot add participants to direct conversations")
	}

	var req struct {
		Username string `json:"username"`
	}
	if err := readJSON(c, &req); err != nil || req.Username == "" {
		return writeError(c, fiber.StatusBadRequest, "username is required")
	}

	var uid int64
	err := h.db.QueryRow(ctx, `SELECT id FROM users WHERE username = $1`, req.Username).Scan(&uid)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "user not found")
	}

	_, err = h.db.Exec(ctx,
		`INSERT INTO conversation_participants (conversation_id, user_id, role) VALUES ($1, $2, 'member') ON CONFLICT DO NOTHING`,
		convoID, uid)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to add participant")
	}

	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) RemoveParticipant(c *fiber.Ctx) error {
	claims := getClaims(c)
	convoID, _ := strconv.ParseInt(c.Params("conversationId"), 10, 64)
	username := c.Params("username")
	ctx := context.Background()

	// Verify the requester is a participant and get their role
	var requesterRole string
	err := h.db.QueryRow(ctx,
		`SELECT role FROM conversation_participants WHERE conversation_id=$1 AND user_id=$2`,
		convoID, claims.UserID).Scan(&requesterRole)
	if err != nil {
		return writeError(c, fiber.StatusForbidden, "not a participant")
	}

	// Get target user ID
	var targetID int64
	err = h.db.QueryRow(ctx, `SELECT id FROM users WHERE username = $1`, username).Scan(&targetID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "user not found")
	}

	// Users can remove themselves; otherwise must be owner
	if targetID != claims.UserID && requesterRole != "owner" {
		return writeError(c, fiber.StatusForbidden, "only the owner can remove other participants")
	}

	_, err = h.db.Exec(ctx,
		`DELETE FROM conversation_participants WHERE conversation_id=$1 AND user_id=$2`,
		convoID, targetID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to remove participant")
	}

	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) DeleteConversation(c *fiber.Ctx) error {
	claims := getClaims(c)
	convoID, _ := strconv.ParseInt(c.Params("conversationId"), 10, 64)
	ctx := context.Background()

	// Only the owner can delete a conversation
	var role string
	err := h.db.QueryRow(ctx,
		`SELECT role FROM conversation_participants WHERE conversation_id=$1 AND user_id=$2`,
		convoID, claims.UserID).Scan(&role)
	if err != nil {
		return writeError(c, fiber.StatusForbidden, "not a participant")
	}
	if role != "owner" {
		return writeError(c, fiber.StatusForbidden, "only the owner can delete the conversation")
	}

	// Delete cascade should handle messages, participants, reactions
	_, err = h.db.Exec(ctx, `DELETE FROM conversations WHERE id = $1`, convoID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to delete conversation")
	}

	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) SearchMessages(c *fiber.Ctx) error {
	claims := getClaims(c)
	convoID, _ := strconv.ParseInt(c.Params("conversationId"), 10, 64)
	query := c.Query("q")
	ctx := context.Background()

	if query == "" || len(query) < 2 {
		return writeJSON(c, fiber.StatusOK, []domain.Message{})
	}
	// Escape ILIKE wildcards to prevent pattern injection
	query = strings.NewReplacer("%", "\\%", "_", "\\_").Replace(query)

	// Verify participant
	var count int
	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM conversation_participants WHERE conversation_id=$1 AND user_id=$2`,
		convoID, claims.UserID).Scan(&count)
	if count == 0 {
		return writeError(c, fiber.StatusForbidden, "not a participant")
	}

	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	rows, err := h.db.Query(ctx,
		`SELECT m.id, m.conversation_id, m.sender_id, u.username, u.full_name,
		        m.content, m.type, m.reply_to_id, m.edited, m.created_at, m.updated_at
		 FROM chat_messages m
		 JOIN users u ON u.id = m.sender_id
		 WHERE m.conversation_id = $1 AND m.content ILIKE '%' || $2 || '%'
		 ORDER BY m.created_at DESC LIMIT $3`,
		convoID, query, limit)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "search failed")
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

	return writeJSON(c, fiber.StatusOK, msgs)
}
