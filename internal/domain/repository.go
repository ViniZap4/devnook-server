package domain

import "time"

type Repository struct {
	ID          int64     `json:"id"`
	OwnerID     int64     `json:"owner_id"`
	Owner       string    `json:"owner"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	IsPrivate   bool      `json:"is_private"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
