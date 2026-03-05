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
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS users (
			id         BIGSERIAL PRIMARY KEY,
			username   TEXT UNIQUE NOT NULL,
			email      TEXT UNIQUE NOT NULL,
			password   TEXT NOT NULL,
			full_name  TEXT NOT NULL DEFAULT '',
			avatar_url TEXT NOT NULL DEFAULT '',
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
			id      BIGSERIAL PRIMARY KEY,
			org_id  BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
			user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			role    TEXT NOT NULL DEFAULT 'member',
			joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(org_id, user_id)
		);

		CREATE TABLE IF NOT EXISTS repositories (
			id             BIGSERIAL PRIMARY KEY,
			owner_id       BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			org_id         BIGINT REFERENCES organizations(id) ON DELETE CASCADE,
			name           TEXT NOT NULL,
			description    TEXT NOT NULL DEFAULT '',
			is_private     BOOLEAN NOT NULL DEFAULT false,
			default_branch TEXT NOT NULL DEFAULT 'main',
			created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
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

		CREATE TABLE IF NOT EXISTS issues (
			id          BIGSERIAL PRIMARY KEY,
			repo_id     BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
			number      INT NOT NULL,
			author_id   BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			title       TEXT NOT NULL,
			body        TEXT NOT NULL DEFAULT '',
			state       TEXT NOT NULL DEFAULT 'open',
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
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
			id      BIGSERIAL PRIMARY KEY,
			repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
			name    TEXT NOT NULL,
			color   TEXT NOT NULL DEFAULT '#cccccc',
			UNIQUE(repo_id, name)
		);

		CREATE TABLE IF NOT EXISTS issue_labels (
			issue_id BIGINT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			label_id BIGINT NOT NULL REFERENCES labels(id) ON DELETE CASCADE,
			PRIMARY KEY (issue_id, label_id)
		);

		CREATE TABLE IF NOT EXISTS user_preferences (
			user_id  BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
			theme    TEXT NOT NULL DEFAULT 'default-dark',
			mode     TEXT NOT NULL DEFAULT 'dark',
			locale   TEXT NOT NULL DEFAULT 'en',
			settings JSONB NOT NULL DEFAULT '{}'
		);

		-- Migration helpers for existing databases
		ALTER TABLE users ADD COLUMN IF NOT EXISTS is_admin BOOLEAN NOT NULL DEFAULT false;
		ALTER TABLE repositories ADD COLUMN IF NOT EXISTS default_branch TEXT NOT NULL DEFAULT 'main';
		ALTER TABLE repositories ADD COLUMN IF NOT EXISTS org_id BIGINT REFERENCES organizations(id) ON DELETE CASCADE;
	`)
	return err
}
