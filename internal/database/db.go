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
		ALTER TABLE labels ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '';
	`)
	return err
}
