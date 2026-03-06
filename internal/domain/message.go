package domain

import "time"

type Conversation struct {
	ID           int64                     `json:"id"`
	Type         string                    `json:"type"`
	Name         string                    `json:"name"`
	Participants []ConversationParticipant  `json:"participants"`
	LastMessage  *Message                  `json:"last_message"`
	UnreadCount  int                       `json:"unread_count"`
	RepoOwner    *string                   `json:"repo_owner"`
	RepoName     *string                   `json:"repo_name"`
	OrgName      *string                   `json:"org_name"`
	IssueNumber  *int                      `json:"issue_number"`
	CreatedAt    time.Time                 `json:"created_at"`
	UpdatedAt    time.Time                 `json:"updated_at"`
}

type ConversationParticipant struct {
	UserID    int64  `json:"user_id"`
	Username  string `json:"username"`
	FullName  string `json:"full_name"`
	AvatarURL string `json:"avatar_url"`
	Role      string `json:"role"`
}

type Message struct {
	ID             int64             `json:"id"`
	ConversationID int64             `json:"conversation_id"`
	SenderID       int64             `json:"sender_id"`
	SenderUsername string            `json:"sender_username"`
	SenderFullName string            `json:"sender_full_name"`
	Content        string            `json:"content"`
	Type           string            `json:"type"`
	ReplyToID      *int64            `json:"reply_to_id"`
	Reactions      []MessageReaction `json:"reactions"`
	Edited         bool              `json:"edited"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

type MessageReaction struct {
	Emoji   string `json:"emoji"`
	Count   int    `json:"count"`
	Reacted bool   `json:"reacted"`
}
