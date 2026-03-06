package domain

import "time"

type User struct {
	ID        int64     `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	Password  string    `json:"-"`
	FullName  string    `json:"full_name"`
	AvatarURL string    `json:"avatar_url"`
	Bio       string    `json:"bio"`
	Location  string    `json:"location"`
	Website   string    `json:"website"`
	IsAdmin   bool      `json:"is_admin"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type UserStatus struct {
	Emoji   string `json:"emoji"`
	Message string `json:"message"`
	Busy    bool   `json:"busy"`
}
