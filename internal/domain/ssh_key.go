package domain

import "time"

type SSHKey struct {
	ID          int64     `json:"id"`
	UserID      int64     `json:"user_id"`
	Name        string    `json:"name"`
	Fingerprint string    `json:"fingerprint"`
	Content     string    `json:"content"`
	CreatedAt   time.Time `json:"created_at"`
}
