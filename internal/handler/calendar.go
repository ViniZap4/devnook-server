package handler

import (
	"context"
	"sort"
	"strconv"
	"time"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/gofiber/fiber/v2"
)

// --- calendar_events CRUD ---

func (h *Handler) ListCalendarEvents(c *fiber.Ctx) error {
	claims := getClaims(c)
	ctx := context.Background()

	args := []any{claims.UserID}
	query := `SELECT id, user_id, title, description, type, start_time, end_time,
	                 all_day, color, recurrence, project_id, sprint_id,
	                 milestone_id, issue_id, conversation_id, created_at, updated_at
	          FROM calendar_events
	          WHERE user_id = $1`
	argIdx := 2

	if s := c.Query("start"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			query += " AND start_time >= $" + strconv.Itoa(argIdx)
			args = append(args, t)
			argIdx++
		}
	}
	if e := c.Query("end"); e != "" {
		if t, err := time.Parse(time.RFC3339, e); err == nil {
			query += " AND start_time <= $" + strconv.Itoa(argIdx)
			args = append(args, t)
			argIdx++
		}
	}
	if typ := c.Query("type"); typ != "" {
		query += " AND type = $" + strconv.Itoa(argIdx)
		args = append(args, typ)
		argIdx++
	}
	_ = argIdx
	query += " ORDER BY start_time"

	rows, err := h.db.Query(ctx, query, args...)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to list calendar events")
	}
	defer rows.Close()

	events := []domain.CalendarEvent{}
	for rows.Next() {
		var e domain.CalendarEvent
		if err := rows.Scan(
			&e.ID, &e.UserID, &e.Title, &e.Description, &e.Type,
			&e.StartTime, &e.EndTime, &e.AllDay, &e.Color, &e.Recurrence,
			&e.ProjectID, &e.SprintID, &e.MilestoneID, &e.IssueID,
			&e.ConversationID, &e.CreatedAt, &e.UpdatedAt,
		); err != nil {
			continue
		}
		e.Attendees = []domain.EventAttendee{}
		events = append(events, e)
	}
	return writeJSON(c, fiber.StatusOK, events)
}

type calendarEventRequest struct {
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	Type           string   `json:"type"`
	StartTime      string   `json:"start_time"`
	EndTime        string   `json:"end_time"`
	AllDay         bool     `json:"all_day"`
	Color          string   `json:"color"`
	Recurrence     string   `json:"recurrence"`
	ProjectID      *int64   `json:"project_id"`
	SprintID       *int64   `json:"sprint_id"`
	MilestoneID    *int64   `json:"milestone_id"`
	IssueID        *int64   `json:"issue_id"`
	ConversationID *int64   `json:"conversation_id"`
	Attendees      []string `json:"attendees"`
}

func (h *Handler) CreateCalendarEvent(c *fiber.Ctx) error {
	claims := getClaims(c)
	ctx := context.Background()

	var req calendarEventRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Title == "" {
		return writeError(c, fiber.StatusBadRequest, "title is required")
	}
	if req.StartTime == "" {
		return writeError(c, fiber.StatusBadRequest, "start_time is required")
	}

	startTime, err := time.Parse(time.RFC3339, req.StartTime)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid start_time format, use RFC3339")
	}
	var endTime *time.Time
	if req.EndTime != "" {
		t, err := time.Parse(time.RFC3339, req.EndTime)
		if err != nil {
			return writeError(c, fiber.StatusBadRequest, "invalid end_time format, use RFC3339")
		}
		endTime = &t
	}

	eventType := req.Type
	if eventType == "" {
		eventType = "event"
	}

	tx, err := h.db.Begin(ctx)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to begin transaction")
	}
	defer tx.Rollback(ctx)

	var id int64
	err = tx.QueryRow(ctx,
		`INSERT INTO calendar_events
		 (user_id, title, description, type, start_time, end_time, all_day,
		  color, recurrence, project_id, sprint_id, milestone_id, issue_id, conversation_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		 RETURNING id`,
		claims.UserID, req.Title, req.Description, eventType,
		startTime, endTime, req.AllDay, req.Color, req.Recurrence,
		req.ProjectID, req.SprintID, req.MilestoneID, req.IssueID, req.ConversationID,
	).Scan(&id)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to create calendar event")
	}

	for _, username := range req.Attendees {
		attendeeID, err := h.resolveUserID(ctx, username)
		if err != nil {
			continue
		}
		tx.Exec(ctx,
			`INSERT INTO calendar_event_attendees (event_id, user_id, status)
			 VALUES ($1, $2, 'pending') ON CONFLICT DO NOTHING`,
			id, attendeeID)
	}

	if err := tx.Commit(ctx); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to commit transaction")
	}
	return writeJSON(c, fiber.StatusCreated, map[string]any{"id": id})
}

func (h *Handler) GetCalendarEvent(c *fiber.Ctx) error {
	claims := getClaims(c)
	ctx := context.Background()

	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid event id")
	}

	var e domain.CalendarEvent
	err = h.db.QueryRow(ctx,
		`SELECT id, user_id, title, description, type, start_time, end_time,
		        all_day, color, recurrence, project_id, sprint_id,
		        milestone_id, issue_id, conversation_id, created_at, updated_at
		 FROM calendar_events
		 WHERE id = $1 AND user_id = $2`, id, claims.UserID).Scan(
		&e.ID, &e.UserID, &e.Title, &e.Description, &e.Type,
		&e.StartTime, &e.EndTime, &e.AllDay, &e.Color, &e.Recurrence,
		&e.ProjectID, &e.SprintID, &e.MilestoneID, &e.IssueID,
		&e.ConversationID, &e.CreatedAt, &e.UpdatedAt,
	)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "event not found")
	}

	aRows, aErr := h.db.Query(ctx,
		`SELECT u.id, u.username, u.full_name, u.avatar_url, a.status
		 FROM calendar_event_attendees a
		 JOIN users u ON u.id = a.user_id
		 WHERE a.event_id = $1`, id)
	if aErr == nil {
		defer aRows.Close()
		for aRows.Next() {
			var a domain.EventAttendee
			if err := aRows.Scan(&a.UserID, &a.Username, &a.FullName, &a.AvatarURL, &a.Status); err != nil {
				continue
			}
			e.Attendees = append(e.Attendees, a)
		}
	}
	if e.Attendees == nil {
		e.Attendees = []domain.EventAttendee{}
	}
	return writeJSON(c, fiber.StatusOK, e)
}

type updateCalendarEventRequest struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Type        *string `json:"type"`
	StartTime   *string `json:"start_time"`
	EndTime     *string `json:"end_time"`
	AllDay      *bool   `json:"all_day"`
	Color       *string `json:"color"`
	Recurrence  *string `json:"recurrence"`
}

func (h *Handler) UpdateCalendarEvent(c *fiber.Ctx) error {
	claims := getClaims(c)
	ctx := context.Background()

	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid event id")
	}

	var req updateCalendarEventRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}

	var startTime *time.Time
	if req.StartTime != nil {
		t, err := time.Parse(time.RFC3339, *req.StartTime)
		if err != nil {
			return writeError(c, fiber.StatusBadRequest, "invalid start_time format, use RFC3339")
		}
		startTime = &t
	}
	var endTime *time.Time
	if req.EndTime != nil {
		t, err := time.Parse(time.RFC3339, *req.EndTime)
		if err != nil {
			return writeError(c, fiber.StatusBadRequest, "invalid end_time format, use RFC3339")
		}
		endTime = &t
	}

	tag, err := h.db.Exec(ctx,
		`UPDATE calendar_events SET
		    title       = COALESCE($1, title),
		    description = COALESCE($2, description),
		    type        = COALESCE($3, type),
		    start_time  = COALESCE($4, start_time),
		    end_time    = COALESCE($5, end_time),
		    all_day     = COALESCE($6, all_day),
		    color       = COALESCE($7, color),
		    recurrence  = COALESCE($8, recurrence),
		    updated_at  = NOW()
		 WHERE id = $9 AND user_id = $10`,
		req.Title, req.Description, req.Type, startTime, endTime,
		req.AllDay, req.Color, req.Recurrence, id, claims.UserID)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "event not found")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) DeleteCalendarEvent(c *fiber.Ctx) error {
	claims := getClaims(c)
	ctx := context.Background()

	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid event id")
	}

	tag, err := h.db.Exec(ctx,
		`DELETE FROM calendar_events WHERE id = $1 AND user_id = $2`, id, claims.UserID)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "event not found")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// --- RSVP ---

type rsvpRequest struct {
	Status string `json:"status"`
}

func (h *Handler) UpdateCalendarRSVP(c *fiber.Ctx) error {
	claims := getClaims(c)
	ctx := context.Background()

	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid event id")
	}

	var req rsvpRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Status == "" {
		return writeError(c, fiber.StatusBadRequest, "status is required")
	}

	tag, err := h.db.Exec(ctx,
		`UPDATE calendar_event_attendees SET status = $1
		 WHERE event_id = $2 AND user_id = $3`,
		req.Status, id, claims.UserID)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "attendee record not found")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// --- Unified Calendar ---

func (h *Handler) GetUnifiedCalendar(c *fiber.Ctx) error {
	claims := getClaims(c)
	ctx := context.Background()

	var start, end *time.Time
	if s := c.Query("start"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			start = &t
		}
	}
	if e := c.Query("end"); e != "" {
		if t, err := time.Parse(time.RFC3339, e); err == nil {
			end = &t
		}
	}

	var entries []domain.CalendarEntry

	// 1. User's own calendar_events
	{
		args := []any{claims.UserID}
		q := `SELECT id, title, start_time, end_time, all_day, color
		      FROM calendar_events WHERE user_id = $1`
		argIdx := 2
		if start != nil {
			q += " AND start_time >= $" + strconv.Itoa(argIdx)
			args = append(args, *start)
			argIdx++
		}
		if end != nil {
			q += " AND start_time <= $" + strconv.Itoa(argIdx)
			args = append(args, *end)
			argIdx++
		}
		_ = argIdx

		rows, err := h.db.Query(ctx, q, args...)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var color string
				var e domain.CalendarEntry
				if err := rows.Scan(&e.ID, &e.Title, &e.StartTime, &e.EndTime, &e.AllDay, &color); err != nil {
					continue
				}
				if color == "" {
					color = "#6366f1"
				}
				e.Color = color
				e.Source = "event"
				e.Link = ""
				entries = append(entries, e)
			}
		}
	}

	// 2. Sprints from projects where the user is a member
	{
		args := []any{claims.UserID}
		q := `SELECT s.id, s.name, s.start_date, s.end_date, p.slug
		      FROM project_sprints s
		      JOIN projects p ON p.id = s.project_id
		      JOIN project_members pm ON pm.project_id = p.id
		      WHERE pm.user_id = $1
		        AND (s.start_date IS NOT NULL OR s.end_date IS NOT NULL)`
		argIdx := 2
		if start != nil {
			q += " AND (s.end_date IS NULL OR s.end_date >= $" + strconv.Itoa(argIdx) + ")"
			args = append(args, *start)
			argIdx++
		}
		if end != nil {
			q += " AND (s.start_date IS NULL OR s.start_date <= $" + strconv.Itoa(argIdx) + ")"
			args = append(args, *end)
			argIdx++
		}
		_ = argIdx

		rows, err := h.db.Query(ctx, q, args...)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var sprintID int64
				var name, projectSlug string
				var sprintStart, sprintEnd *time.Time
				if err := rows.Scan(&sprintID, &name, &sprintStart, &sprintEnd, &projectSlug); err != nil {
					continue
				}
				link := "/projects/" + projectSlug + "/sprints/" + strconv.FormatInt(sprintID, 10)
				slug := projectSlug

				if sprintStart != nil {
					e := domain.CalendarEntry{
						ID:          sprintID,
						Title:       name + " (sprint start)",
						StartTime:   *sprintStart,
						EndTime:     sprintEnd,
						AllDay:      true,
						Color:       "#10b981",
						Source:      "sprint",
						Link:        link,
						ProjectSlug: &slug,
					}
					entries = append(entries, e)
				}
				// Emit a separate end entry when the end date differs from the start date
				if sprintEnd != nil && (sprintStart == nil || !sprintStart.Equal(*sprintEnd)) {
					slugCopy := projectSlug
					e := domain.CalendarEntry{
						ID:          sprintID,
						Title:       name + " (sprint end)",
						StartTime:   *sprintEnd,
						AllDay:      true,
						Color:       "#10b981",
						Source:      "sprint",
						Link:        link,
						ProjectSlug: &slugCopy,
					}
					entries = append(entries, e)
				}
			}
		}
	}

	// 3. Milestone due_dates from repos the user owns or collaborates on
	{
		args := []any{claims.UserID}
		q := `SELECT m.id, m.title, m.due_date, u.username, r.name
		      FROM milestones m
		      JOIN repositories r ON r.id = m.repo_id
		      JOIN users u ON u.id = r.owner_id
		      WHERE m.due_date IS NOT NULL
		        AND (r.owner_id = $1
		             OR EXISTS (
		                 SELECT 1 FROM repo_collaborators rc
		                 WHERE rc.repo_id = r.id AND rc.user_id = $1
		             ))`
		argIdx := 2
		if start != nil {
			q += " AND m.due_date >= $" + strconv.Itoa(argIdx)
			args = append(args, *start)
			argIdx++
		}
		if end != nil {
			q += " AND m.due_date <= $" + strconv.Itoa(argIdx)
			args = append(args, *end)
			argIdx++
		}
		_ = argIdx

		rows, err := h.db.Query(ctx, q, args...)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var mID int64
				var title, ownerUsername, repoName string
				var dueDate time.Time
				if err := rows.Scan(&mID, &title, &dueDate, &ownerUsername, &repoName); err != nil {
					continue
				}
				owner := ownerUsername
				repo := repoName
				link := "/" + ownerUsername + "/" + repoName
				e := domain.CalendarEntry{
					ID:        mID,
					Title:     title + " (milestone)",
					StartTime: dueDate,
					AllDay:    true,
					Color:     "#f59e0b",
					Source:    "milestone",
					Link:      link,
					RepoOwner: &owner,
					RepoName:  &repo,
				}
				entries = append(entries, e)
			}
		}
	}

	// 4. Issue due_dates via project_items that are linked to issues, for projects
	//    the user is a member of. The issue number and repo are used to build the link.
	{
		args := []any{claims.UserID}
		q := `SELECT pi.id, pi.title, pi.due_date, u.username, r.name, i.number
		      FROM project_items pi
		      JOIN projects p ON p.id = pi.project_id
		      JOIN project_members pm ON pm.project_id = p.id AND pm.user_id = $1
		      JOIN issues i ON i.id = pi.issue_id
		      JOIN repositories r ON r.id = i.repo_id
		      JOIN users u ON u.id = r.owner_id
		      WHERE pi.due_date IS NOT NULL
		        AND pi.issue_id IS NOT NULL`
		argIdx := 2
		if start != nil {
			q += " AND pi.due_date >= $" + strconv.Itoa(argIdx)
			args = append(args, *start)
			argIdx++
		}
		if end != nil {
			q += " AND pi.due_date <= $" + strconv.Itoa(argIdx)
			args = append(args, *end)
			argIdx++
		}
		_ = argIdx

		rows, err := h.db.Query(ctx, q, args...)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var itemID int64
				var title, ownerUsername, repoName string
				var dueDate time.Time
				var issueNumber int
				if err := rows.Scan(&itemID, &title, &dueDate, &ownerUsername, &repoName, &issueNumber); err != nil {
					continue
				}
				owner := ownerUsername
				repo := repoName
				link := "/" + ownerUsername + "/" + repoName + "/issues/" + strconv.Itoa(issueNumber)
				e := domain.CalendarEntry{
					ID:        itemID,
					Title:     title + " (issue)",
					StartTime: dueDate,
					AllDay:    true,
					Color:     "#ef4444",
					Source:    "issue",
					Link:      link,
					RepoOwner: &owner,
					RepoName:  &repo,
				}
				entries = append(entries, e)
			}
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].StartTime.Before(entries[j].StartTime)
	})

	if entries == nil {
		entries = []domain.CalendarEntry{}
	}
	return writeJSON(c, fiber.StatusOK, entries)
}
