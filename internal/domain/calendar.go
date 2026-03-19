package domain

import "time"

type CalendarEvent struct {
	ID             int64           `json:"id"`
	UserID         int64           `json:"user_id"`
	Title          string          `json:"title"`
	Description    string          `json:"description"`
	Type           string          `json:"type"`
	StartTime      time.Time       `json:"start_time"`
	EndTime        *time.Time      `json:"end_time"`
	AllDay         bool            `json:"all_day"`
	Color          string          `json:"color"`
	Recurrence     string          `json:"recurrence"`
	ProjectID      *int64          `json:"project_id"`
	SprintID       *int64          `json:"sprint_id"`
	MilestoneID    *int64          `json:"milestone_id"`
	IssueID        *int64          `json:"issue_id"`
	ConversationID *int64          `json:"conversation_id"`
	Attendees      []EventAttendee `json:"attendees"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type EventAttendee struct {
	UserID    int64  `json:"user_id"`
	Username  string `json:"username"`
	FullName  string `json:"full_name"`
	AvatarURL string `json:"avatar_url"`
	Status    string `json:"status"`
}

type CalendarEntry struct {
	ID          int64      `json:"id"`
	Title       string     `json:"title"`
	StartTime   time.Time  `json:"start_time"`
	EndTime     *time.Time `json:"end_time"`
	AllDay      bool       `json:"all_day"`
	Color       string     `json:"color"`
	Source      string     `json:"source"`
	Link        string     `json:"link"`
	ProjectSlug *string    `json:"project_slug"`
	RepoOwner   *string    `json:"repo_owner"`
	RepoName    *string    `json:"repo_name"`
}
