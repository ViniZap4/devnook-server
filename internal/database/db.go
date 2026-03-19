package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

func Connect(databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		return nil, fmt.Errorf("unable to connect to database: %w", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("unable to ping database: %w", err)
	}
	return pool, nil
}

func Migrate(pool *pgxpool.Pool) error {
	ctx := context.Background()

	// Core tables
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS users (
			id         BIGSERIAL PRIMARY KEY,
			username   TEXT UNIQUE NOT NULL,
			email      TEXT UNIQUE NOT NULL,
			password   TEXT NOT NULL,
			full_name  TEXT NOT NULL DEFAULT '',
			avatar_url TEXT NOT NULL DEFAULT '',
			bio        TEXT NOT NULL DEFAULT '',
			location   TEXT NOT NULL DEFAULT '',
			website    TEXT NOT NULL DEFAULT '',
			is_admin   BOOLEAN NOT NULL DEFAULT false,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS organizations (
			id           BIGSERIAL PRIMARY KEY,
			name         TEXT UNIQUE NOT NULL,
			display_name TEXT NOT NULL DEFAULT '',
			description  TEXT NOT NULL DEFAULT '',
			avatar_url   TEXT NOT NULL DEFAULT '',
			created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS org_members (
			id        BIGSERIAL PRIMARY KEY,
			org_id    BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
			user_id   BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			role      TEXT NOT NULL DEFAULT 'member',
			joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(org_id, user_id)
		);

		CREATE TABLE IF NOT EXISTS repositories (
			id              BIGSERIAL PRIMARY KEY,
			owner_id        BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			org_id          BIGINT REFERENCES organizations(id) ON DELETE CASCADE,
			name            TEXT NOT NULL,
			description     TEXT NOT NULL DEFAULT '',
			website         TEXT NOT NULL DEFAULT '',
			is_private      BOOLEAN NOT NULL DEFAULT false,
			is_fork         BOOLEAN NOT NULL DEFAULT false,
			forked_from_id  BIGINT REFERENCES repositories(id) ON DELETE SET NULL,
			default_branch  TEXT NOT NULL DEFAULT 'main',
			topics          TEXT[] NOT NULL DEFAULT '{}',
			stars_count     INT NOT NULL DEFAULT 0,
			forks_count     INT NOT NULL DEFAULT 0,
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(owner_id, name)
		);

		CREATE TABLE IF NOT EXISTS shortcuts (
			id         BIGSERIAL PRIMARY KEY,
			user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			title      TEXT NOT NULL,
			url        TEXT NOT NULL,
			icon_url   TEXT NOT NULL DEFAULT '',
			color      TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS milestones (
			id          BIGSERIAL PRIMARY KEY,
			repo_id     BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
			title       TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			state       TEXT NOT NULL DEFAULT 'open',
			due_date    TIMESTAMPTZ,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS issues (
			id           BIGSERIAL PRIMARY KEY,
			repo_id      BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
			number       INT NOT NULL,
			author_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			title        TEXT NOT NULL,
			body         TEXT NOT NULL DEFAULT '',
			state        TEXT NOT NULL DEFAULT 'open',
			milestone_id BIGINT REFERENCES milestones(id) ON DELETE SET NULL,
			assignee_id  BIGINT REFERENCES users(id) ON DELETE SET NULL,
			created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(repo_id, number)
		);

		CREATE TABLE IF NOT EXISTS issue_comments (
			id         BIGSERIAL PRIMARY KEY,
			issue_id   BIGINT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			author_id  BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			body       TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS labels (
			id          BIGSERIAL PRIMARY KEY,
			repo_id     BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
			name        TEXT NOT NULL,
			color       TEXT NOT NULL DEFAULT '#cccccc',
			description TEXT NOT NULL DEFAULT '',
			UNIQUE(repo_id, name)
		);

		CREATE TABLE IF NOT EXISTS issue_labels (
			issue_id BIGINT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			label_id BIGINT NOT NULL REFERENCES labels(id) ON DELETE CASCADE,
			PRIMARY KEY (issue_id, label_id)
		);

		CREATE TABLE IF NOT EXISTS stars (
			user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			repo_id    BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (user_id, repo_id)
		);

		CREATE TABLE IF NOT EXISTS releases (
			id            BIGSERIAL PRIMARY KEY,
			repo_id       BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
			tag_name      TEXT NOT NULL,
			title         TEXT NOT NULL,
			body          TEXT NOT NULL DEFAULT '',
			is_draft      BOOLEAN NOT NULL DEFAULT false,
			is_prerelease BOOLEAN NOT NULL DEFAULT false,
			author_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(repo_id, tag_name)
		);

		CREATE TABLE IF NOT EXISTS pull_requests (
			id          BIGSERIAL PRIMARY KEY,
			repo_id     BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
			number      INT NOT NULL,
			author_id   BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			title       TEXT NOT NULL,
			body        TEXT NOT NULL DEFAULT '',
			state       TEXT NOT NULL DEFAULT 'open',
			head_branch TEXT NOT NULL,
			base_branch TEXT NOT NULL,
			merged_at   TIMESTAMPTZ,
			merged_by   BIGINT REFERENCES users(id),
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(repo_id, number)
		);

		CREATE TABLE IF NOT EXISTS pr_comments (
			id         BIGSERIAL PRIMARY KEY,
			pr_id      BIGINT NOT NULL REFERENCES pull_requests(id) ON DELETE CASCADE,
			author_id  BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			body       TEXT NOT NULL,
			path       TEXT,
			line       INT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS pr_reviews (
			id         BIGSERIAL PRIMARY KEY,
			pr_id      BIGINT NOT NULL REFERENCES pull_requests(id) ON DELETE CASCADE,
			author_id  BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			state      TEXT NOT NULL DEFAULT 'pending',
			body       TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS ssh_keys (
			id          BIGSERIAL PRIMARY KEY,
			user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name        TEXT NOT NULL,
			fingerprint TEXT NOT NULL,
			content     TEXT NOT NULL,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS webhooks (
			id         BIGSERIAL PRIMARY KEY,
			repo_id    BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
			url        TEXT NOT NULL,
			secret     TEXT NOT NULL DEFAULT '',
			events     TEXT[] NOT NULL DEFAULT '{push}',
			active     BOOLEAN NOT NULL DEFAULT true,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS notifications (
			id         BIGSERIAL PRIMARY KEY,
			user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			repo_id    BIGINT REFERENCES repositories(id) ON DELETE CASCADE,
			type       TEXT NOT NULL,
			title      TEXT NOT NULL,
			body       TEXT NOT NULL DEFAULT '',
			read       BOOLEAN NOT NULL DEFAULT false,
			link       TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS user_preferences (
			user_id  BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
			theme    TEXT NOT NULL DEFAULT 'default-dark',
			mode     TEXT NOT NULL DEFAULT 'dark',
			locale   TEXT NOT NULL DEFAULT 'en',
			settings JSONB NOT NULL DEFAULT '{}'
		);

		CREATE TABLE IF NOT EXISTS repo_collaborators (
			id         BIGSERIAL PRIMARY KEY,
			repo_id    BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
			user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			permission TEXT NOT NULL DEFAULT 'write',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(repo_id, user_id)
		);

		CREATE TABLE IF NOT EXISTS follows (
			follower_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			following_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (follower_id, following_id),
			CHECK (follower_id != following_id)
		);

		CREATE TABLE IF NOT EXISTS blocks (
			blocker_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			blocked_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (blocker_id, blocked_id),
			CHECK (blocker_id != blocked_id)
		);

		CREATE TABLE IF NOT EXISTS user_status (
			user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
			emoji   TEXT NOT NULL DEFAULT '',
			message TEXT NOT NULL DEFAULT '',
			busy    BOOLEAN NOT NULL DEFAULT false,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS posts (
			id           BIGSERIAL PRIMARY KEY,
			author_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			content      TEXT NOT NULL,
			type         TEXT NOT NULL DEFAULT 'text',
			repo_owner   TEXT,
			repo_name    TEXT,
			commit_hash  TEXT,
			issue_number INT,
			org_name     TEXT,
			tags         TEXT[] NOT NULL DEFAULT '{}',
			created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS post_likes (
			user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			post_id    BIGINT NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (user_id, post_id)
		);

		CREATE TABLE IF NOT EXISTS post_comments (
			id         BIGSERIAL PRIMARY KEY,
			post_id    BIGINT NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
			author_id  BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			content    TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS post_reposts (
			user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			post_id    BIGINT NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (user_id, post_id)
		);

		CREATE TABLE IF NOT EXISTS conversations (
			id           BIGSERIAL PRIMARY KEY,
			type         TEXT NOT NULL DEFAULT 'direct',
			name         TEXT NOT NULL DEFAULT '',
			repo_owner   TEXT,
			repo_name    TEXT,
			org_name     TEXT,
			issue_number INT,
			created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS conversation_participants (
			conversation_id BIGINT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
			user_id         BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			role            TEXT NOT NULL DEFAULT 'member',
			last_read_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (conversation_id, user_id)
		);

		CREATE TABLE IF NOT EXISTS chat_messages (
			id              BIGSERIAL PRIMARY KEY,
			conversation_id BIGINT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
			sender_id       BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			content         TEXT NOT NULL,
			type            TEXT NOT NULL DEFAULT 'text',
			reply_to_id     BIGINT REFERENCES chat_messages(id) ON DELETE SET NULL,
			edited          BOOLEAN NOT NULL DEFAULT false,
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS message_reactions (
			id         BIGSERIAL PRIMARY KEY,
			message_id BIGINT NOT NULL REFERENCES chat_messages(id) ON DELETE CASCADE,
			user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			emoji      TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(message_id, user_id, emoji)
		);

		CREATE TABLE IF NOT EXISTS link_previews (
			id          BIGSERIAL PRIMARY KEY,
			url         TEXT UNIQUE NOT NULL,
			title       TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			image_url   TEXT NOT NULL DEFAULT '',
			domain      TEXT NOT NULL DEFAULT '',
			fetched_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS doc_spaces (
			id          BIGSERIAL PRIMARY KEY,
			name        TEXT NOT NULL,
			slug        TEXT UNIQUE NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			icon        TEXT NOT NULL DEFAULT '',
			owner_type  TEXT NOT NULL DEFAULT 'user',
			owner_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			owner_name  TEXT NOT NULL DEFAULT '',
			repo_owner  TEXT,
			repo_name   TEXT,
			org_name    TEXT,
			is_public   BOOLEAN NOT NULL DEFAULT false,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS doc_pages (
			id              BIGSERIAL PRIMARY KEY,
			space_id        BIGINT NOT NULL REFERENCES doc_spaces(id) ON DELETE CASCADE,
			parent_id       BIGINT REFERENCES doc_pages(id) ON DELETE SET NULL,
			title           TEXT NOT NULL,
			slug            TEXT NOT NULL,
			content         TEXT NOT NULL DEFAULT '',
			icon            TEXT NOT NULL DEFAULT '',
			author_id       BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			last_edited_by  BIGINT REFERENCES users(id) ON DELETE SET NULL,
			position        INT NOT NULL DEFAULT 0,
			is_published    BOOLEAN NOT NULL DEFAULT true,
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(space_id, slug)
		);

		CREATE TABLE IF NOT EXISTS doc_page_versions (
			id          BIGSERIAL PRIMARY KEY,
			page_id     BIGINT NOT NULL REFERENCES doc_pages(id) ON DELETE CASCADE,
			title       TEXT NOT NULL,
			content     TEXT NOT NULL,
			author_id   BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			commit_hash TEXT,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS projects (
			id              BIGSERIAL PRIMARY KEY,
			owner_id        BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			org_id          BIGINT REFERENCES organizations(id) ON DELETE CASCADE,
			name            TEXT NOT NULL,
			slug            TEXT NOT NULL,
			description     TEXT NOT NULL DEFAULT '',
			methodology     TEXT NOT NULL DEFAULT 'kanban',
			visibility      TEXT NOT NULL DEFAULT 'private',
			default_view    TEXT NOT NULL DEFAULT 'board',
			color           TEXT NOT NULL DEFAULT '#6366f1',
			icon            TEXT NOT NULL DEFAULT '',
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(owner_id, slug)
		);

		CREATE TABLE IF NOT EXISTS project_members (
			id         BIGSERIAL PRIMARY KEY,
			project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			role       TEXT NOT NULL DEFAULT 'member',
			joined_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(project_id, user_id)
		);

		CREATE TABLE IF NOT EXISTS project_repos (
			project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			repo_id    BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
			added_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (project_id, repo_id)
		);

		CREATE TABLE IF NOT EXISTS project_columns (
			id         BIGSERIAL PRIMARY KEY,
			project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			name       TEXT NOT NULL,
			color      TEXT NOT NULL DEFAULT '#6b7280',
			position   INT NOT NULL DEFAULT 0,
			wip_limit  INT NOT NULL DEFAULT 0,
			is_done    BOOLEAN NOT NULL DEFAULT false,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS project_swimlanes (
			id         BIGSERIAL PRIMARY KEY,
			project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			name       TEXT NOT NULL,
			position   INT NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS project_sprints (
			id           BIGSERIAL PRIMARY KEY,
			project_id   BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			name         TEXT NOT NULL,
			goal         TEXT NOT NULL DEFAULT '',
			number       INT NOT NULL,
			start_date   TIMESTAMPTZ,
			end_date     TIMESTAMPTZ,
			state        TEXT NOT NULL DEFAULT 'planning',
			velocity     INT NOT NULL DEFAULT 0,
			created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(project_id, number)
		);

		CREATE TABLE IF NOT EXISTS project_items (
			id            BIGSERIAL PRIMARY KEY,
			project_id    BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			column_id     BIGINT NOT NULL REFERENCES project_columns(id) ON DELETE CASCADE,
			swimlane_id   BIGINT REFERENCES project_swimlanes(id) ON DELETE SET NULL,
			sprint_id     BIGINT REFERENCES project_sprints(id) ON DELETE SET NULL,
			issue_id      BIGINT REFERENCES issues(id) ON DELETE CASCADE,
			pr_id         BIGINT REFERENCES pull_requests(id) ON DELETE CASCADE,
			title         TEXT NOT NULL DEFAULT '',
			body          TEXT NOT NULL DEFAULT '',
			type          TEXT NOT NULL DEFAULT 'task',
			priority      TEXT NOT NULL DEFAULT 'medium',
			story_points  INT NOT NULL DEFAULT 0,
			assignee_id   BIGINT REFERENCES users(id) ON DELETE SET NULL,
			position      INT NOT NULL DEFAULT 0,
			due_date      TIMESTAMPTZ,
			started_at    TIMESTAMPTZ,
			completed_at  TIMESTAMPTZ,
			created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS project_item_labels (
			item_id  BIGINT NOT NULL REFERENCES project_items(id) ON DELETE CASCADE,
			label_id BIGINT NOT NULL REFERENCES labels(id) ON DELETE CASCADE,
			PRIMARY KEY (item_id, label_id)
		);

		CREATE TABLE IF NOT EXISTS project_item_history (
			id         BIGSERIAL PRIMARY KEY,
			item_id    BIGINT NOT NULL REFERENCES project_items(id) ON DELETE CASCADE,
			user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			field      TEXT NOT NULL,
			old_value  TEXT NOT NULL DEFAULT '',
			new_value  TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS calendar_events (
			id              BIGSERIAL PRIMARY KEY,
			user_id         BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			title           TEXT NOT NULL,
			description     TEXT NOT NULL DEFAULT '',
			type            TEXT NOT NULL DEFAULT 'event',
			start_time      TIMESTAMPTZ NOT NULL,
			end_time        TIMESTAMPTZ,
			all_day         BOOLEAN NOT NULL DEFAULT false,
			color           TEXT NOT NULL DEFAULT '',
			recurrence      TEXT NOT NULL DEFAULT '',
			project_id      BIGINT REFERENCES projects(id) ON DELETE SET NULL,
			sprint_id       BIGINT REFERENCES project_sprints(id) ON DELETE SET NULL,
			milestone_id    BIGINT REFERENCES milestones(id) ON DELETE SET NULL,
			issue_id        BIGINT REFERENCES issues(id) ON DELETE SET NULL,
			conversation_id BIGINT REFERENCES conversations(id) ON DELETE SET NULL,
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS calendar_event_attendees (
			event_id BIGINT NOT NULL REFERENCES calendar_events(id) ON DELETE CASCADE,
			user_id  BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			status   TEXT NOT NULL DEFAULT 'pending',
			PRIMARY KEY (event_id, user_id)
		);

		-- Indexes for chat performance
		CREATE INDEX IF NOT EXISTS idx_chat_messages_conversation_id ON chat_messages(conversation_id);
		CREATE INDEX IF NOT EXISTS idx_chat_messages_sender_id ON chat_messages(sender_id);
		CREATE INDEX IF NOT EXISTS idx_conversation_participants_user_id ON conversation_participants(user_id);

		-- Indexes for project performance
		CREATE INDEX IF NOT EXISTS idx_project_items_project_id ON project_items(project_id);
		CREATE INDEX IF NOT EXISTS idx_project_items_column_id ON project_items(column_id);
		CREATE INDEX IF NOT EXISTS idx_project_items_sprint_id ON project_items(sprint_id);
		CREATE INDEX IF NOT EXISTS idx_project_items_assignee_id ON project_items(assignee_id);
		CREATE INDEX IF NOT EXISTS idx_project_item_history_item_id ON project_item_history(item_id);
		CREATE INDEX IF NOT EXISTS idx_project_members_user_id ON project_members(user_id);
		CREATE INDEX IF NOT EXISTS idx_calendar_events_user_id ON calendar_events(user_id);
		CREATE INDEX IF NOT EXISTS idx_calendar_events_start_time ON calendar_events(start_time);
		CREATE INDEX IF NOT EXISTS idx_calendar_events_project_id ON calendar_events(project_id);

		-- Migration helpers for existing databases
		ALTER TABLE users ADD COLUMN IF NOT EXISTS is_admin BOOLEAN NOT NULL DEFAULT false;
		ALTER TABLE users ADD COLUMN IF NOT EXISTS bio TEXT NOT NULL DEFAULT '';
		ALTER TABLE users ADD COLUMN IF NOT EXISTS location TEXT NOT NULL DEFAULT '';
		ALTER TABLE users ADD COLUMN IF NOT EXISTS website TEXT NOT NULL DEFAULT '';
		ALTER TABLE repositories ADD COLUMN IF NOT EXISTS default_branch TEXT NOT NULL DEFAULT 'main';
		ALTER TABLE repositories ADD COLUMN IF NOT EXISTS org_id BIGINT REFERENCES organizations(id) ON DELETE CASCADE;
		ALTER TABLE repositories ADD COLUMN IF NOT EXISTS website TEXT NOT NULL DEFAULT '';
		ALTER TABLE repositories ADD COLUMN IF NOT EXISTS is_fork BOOLEAN NOT NULL DEFAULT false;
		ALTER TABLE repositories ADD COLUMN IF NOT EXISTS forked_from_id BIGINT REFERENCES repositories(id) ON DELETE SET NULL;
		ALTER TABLE repositories ADD COLUMN IF NOT EXISTS topics TEXT[] NOT NULL DEFAULT '{}';
		ALTER TABLE repositories ADD COLUMN IF NOT EXISTS stars_count INT NOT NULL DEFAULT 0;
		ALTER TABLE repositories ADD COLUMN IF NOT EXISTS forks_count INT NOT NULL DEFAULT 0;
		ALTER TABLE issues ADD COLUMN IF NOT EXISTS milestone_id BIGINT REFERENCES milestones(id) ON DELETE SET NULL;
		ALTER TABLE issues ADD COLUMN IF NOT EXISTS assignee_id BIGINT REFERENCES users(id) ON DELETE SET NULL;
		ALTER TABLE issues ADD COLUMN IF NOT EXISTS priority TEXT NOT NULL DEFAULT 'medium';
		ALTER TABLE issues ADD COLUMN IF NOT EXISTS type TEXT NOT NULL DEFAULT 'task';
		ALTER TABLE issues ADD COLUMN IF NOT EXISTS due_date TIMESTAMPTZ;
		ALTER TABLE issues ADD COLUMN IF NOT EXISTS story_points INT NOT NULL DEFAULT 0;
		ALTER TABLE labels ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '';
		ALTER TABLE organizations ADD COLUMN IF NOT EXISTS location TEXT NOT NULL DEFAULT '';
		ALTER TABLE organizations ADD COLUMN IF NOT EXISTS website TEXT NOT NULL DEFAULT '';
	`)
	return err
}
