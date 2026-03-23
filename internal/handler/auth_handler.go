package handler

import (
	"context"

	"github.com/ViniZap4/devnook-server/internal/auth"
	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/gofiber/fiber/v2"
	"golang.org/x/crypto/bcrypt"
)

type registerRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
	FullName string `json:"full_name"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type authResponse struct {
	Token string      `json:"token"`
	User  domain.User `json:"user"`
}

func (h *Handler) NeedsSetup(c *fiber.Ctx) error {
	var count int64
	h.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM users`).Scan(&count)
	return writeJSON(c, fiber.StatusOK, map[string]bool{"needs_setup": count == 0})
}

func (h *Handler) Setup(c *fiber.Ctx) error {
	var count int64
	h.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM users`).Scan(&count)
	if count > 0 {
		return writeError(c, fiber.StatusForbidden, "setup already completed")
	}

	var req registerRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Username == "" || req.Email == "" || req.Password == "" {
		return writeError(c, fiber.StatusBadRequest, "username, email, and password are required")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to hash password")
	}

	var user domain.User
	err = h.db.QueryRow(context.Background(),
		`INSERT INTO users (username, email, password, full_name, is_admin)
		 VALUES ($1, $2, $3, $4, true)
		 RETURNING id, username, email, full_name, avatar_url, bio, location, website, is_admin, created_at, updated_at`,
		req.Username, req.Email, string(hash), req.FullName,
	).Scan(&user.ID, &user.Username, &user.Email, &user.FullName, &user.AvatarURL, &user.Bio, &user.Location, &user.Website, &user.IsAdmin, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return writeError(c, fiber.StatusConflict, "failed to create admin user")
	}

	token, err := auth.GenerateToken(user.ID, user.Username, h.cfg.Secret)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to generate token")
	}

	return writeJSON(c, fiber.StatusCreated, authResponse{Token: token, User: user})
}

func (h *Handler) Register(c *fiber.Ctx) error {
	var req registerRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Username == "" || req.Email == "" || req.Password == "" {
		return writeError(c, fiber.StatusBadRequest, "username, email, and password are required")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to hash password")
	}

	var user domain.User
	err = h.db.QueryRow(context.Background(),
		`INSERT INTO users (username, email, password, full_name)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, username, email, full_name, avatar_url, bio, location, website, is_admin, created_at, updated_at`,
		req.Username, req.Email, string(hash), req.FullName,
	).Scan(&user.ID, &user.Username, &user.Email, &user.FullName, &user.AvatarURL, &user.Bio, &user.Location, &user.Website, &user.IsAdmin, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return writeError(c, fiber.StatusConflict, "username or email already exists")
	}

	token, err := auth.GenerateToken(user.ID, user.Username, h.cfg.Secret)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to generate token")
	}

	return writeJSON(c, fiber.StatusCreated, authResponse{Token: token, User: user})
}

func (h *Handler) Login(c *fiber.Ctx) error {
	var req loginRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}

	var user domain.User
	var hash string
	err := h.db.QueryRow(context.Background(),
		`SELECT id, username, email, password, full_name, avatar_url, bio, location, website, is_admin, created_at, updated_at
		 FROM users WHERE username = $1`, req.Username,
	).Scan(&user.ID, &user.Username, &user.Email, &hash, &user.FullName, &user.AvatarURL, &user.Bio, &user.Location, &user.Website, &user.IsAdmin, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return writeError(c, fiber.StatusUnauthorized, "invalid credentials")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)); err != nil {
		return writeError(c, fiber.StatusUnauthorized, "invalid credentials")
	}

	token, err := auth.GenerateToken(user.ID, user.Username, h.cfg.Secret)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to generate token")
	}

	return writeJSON(c, fiber.StatusOK, authResponse{Token: token, User: user})
}

func (h *Handler) ChangePassword(c *fiber.Ctx) error {
	claims := getClaims(c)

	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.OldPassword == "" || req.NewPassword == "" {
		return writeError(c, fiber.StatusBadRequest, "old_password and new_password are required")
	}
	if len(req.NewPassword) < 6 {
		return writeError(c, fiber.StatusBadRequest, "new password must be at least 6 characters")
	}

	// Verify current password
	var hash string
	err := h.db.QueryRow(context.Background(),
		`SELECT password FROM users WHERE id = $1`, claims.UserID,
	).Scan(&hash)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "user not found")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.OldPassword)); err != nil {
		return writeError(c, fiber.StatusUnauthorized, "current password is incorrect")
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to hash password")
	}

	if _, err = h.db.Exec(context.Background(),
		`UPDATE users SET password = $1, updated_at = NOW() WHERE id = $2`,
		string(newHash), claims.UserID); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to update password")
	}

	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) GetCurrentUser(c *fiber.Ctx) error {
	claims := getClaims(c)
	var user domain.User
	err := h.db.QueryRow(context.Background(),
		`SELECT id, username, email, full_name, avatar_url, bio, location, website, is_admin, created_at, updated_at
		 FROM users WHERE id = $1`, claims.UserID,
	).Scan(&user.ID, &user.Username, &user.Email, &user.FullName, &user.AvatarURL, &user.Bio, &user.Location, &user.Website, &user.IsAdmin, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "user not found")
	}
	return writeJSON(c, fiber.StatusOK, user)
}
