package domain

import "time"

type Project struct {
	ID          int64     `json:"id"`
	OwnerID     int64     `json:"owner_id"`
	OwnerName   string    `json:"owner_name"`
	OrgID       *int64    `json:"org_id"`
	OrgName     *string   `json:"org_name"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description"`
	Methodology string    `json:"methodology"`
	Visibility  string    `json:"visibility"`
	DefaultView string    `json:"default_view"`
	Color       string    `json:"color"`
	Icon        string    `json:"icon"`
	MemberCount int       `json:"member_count"`
	ItemCount   int       `json:"item_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ProjectMember struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	Username  string    `json:"username"`
	FullName  string    `json:"full_name"`
	AvatarURL string    `json:"avatar_url"`
	Role      string    `json:"role"`
	JoinedAt  time.Time `json:"joined_at"`
}

type ProjectColumn struct {
	ID        int64     `json:"id"`
	ProjectID int64     `json:"project_id"`
	Name      string    `json:"name"`
	Color     string    `json:"color"`
	Position  int       `json:"position"`
	WIPLimit  int       `json:"wip_limit"`
	IsDone    bool      `json:"is_done"`
	ItemCount int       `json:"item_count"`
	CreatedAt time.Time `json:"created_at"`
}

type ProjectSwimlane struct {
	ID        int64     `json:"id"`
	ProjectID int64     `json:"project_id"`
	Name      string    `json:"name"`
	Position  int       `json:"position"`
	CreatedAt time.Time `json:"created_at"`
}

type ProjectSprint struct {
	ID          int64      `json:"id"`
	ProjectID   int64      `json:"project_id"`
	Name        string     `json:"name"`
	Goal        string     `json:"goal"`
	Number      int        `json:"number"`
	StartDate   *time.Time `json:"start_date"`
	EndDate     *time.Time `json:"end_date"`
	State       string     `json:"state"`
	Velocity    int        `json:"velocity"`
	TotalItems  int        `json:"total_items"`
	DoneItems   int        `json:"done_items"`
	TotalPoints int        `json:"total_points"`
	DonePoints  int        `json:"done_points"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type ProjectItem struct {
	ID          int64      `json:"id"`
	ProjectID   int64      `json:"project_id"`
	ColumnID    int64      `json:"column_id"`
	ColumnName  string     `json:"column_name"`
	SwimlaneID  *int64     `json:"swimlane_id"`
	SprintID    *int64     `json:"sprint_id"`
	IssueID     *int64     `json:"issue_id"`
	PRID        *int64     `json:"pr_id"`
	Title       string     `json:"title"`
	Body        string     `json:"body"`
	Type        string     `json:"type"`
	Priority    string     `json:"priority"`
	StoryPoints int        `json:"story_points"`
	AssigneeID  *int64     `json:"assignee_id"`
	Assignee    *string    `json:"assignee"`
	Position    int        `json:"position"`
	DueDate     *time.Time `json:"due_date"`
	StartedAt   *time.Time `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
	IssueNumber *int       `json:"issue_number"`
	IssueState  *string    `json:"issue_state"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type ProjectItemHistory struct {
	ID        int64     `json:"id"`
	ItemID    int64     `json:"item_id"`
	UserID    int64     `json:"user_id"`
	Username  string    `json:"username"`
	Field     string    `json:"field"`
	OldValue  string    `json:"old_value"`
	NewValue  string    `json:"new_value"`
	CreatedAt time.Time `json:"created_at"`
}
