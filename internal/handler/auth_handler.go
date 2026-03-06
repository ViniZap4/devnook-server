package handler

import (
	"context"
	"net/http"

	"github.com/ViniZap4/devnook-server/internal/auth"
	"github.com/ViniZap4/devnook-server/internal/domain"
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

func (h *Handler) NeedsSetup(w http.ResponseWriter, r *http.Request) {
	var count int64
	h.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM users`).Scan(&count)
	writeJSON(w, http.StatusOK, map[string]bool{"needs_setup": count == 0})
}

func (h *Handler) Setup(w http.ResponseWriter, r *http.Request) {
	var count int64
	h.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM users`).Scan(&count)
	if count > 0 {
		writeError(w, http.StatusForbidden, "setup already completed")
		return
	}

	var req registerRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Username == "" || req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "username, email, and password are required")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	var user domain.User
	err = h.db.QueryRow(context.Background(),
		`INSERT INTO users (username, email, password, full_name, is_admin)
		 VALUES ($1, $2, $3, $4, true)
		 RETURNING id, username, email, full_name, avatar_url, bio, location, website, is_admin, created_at, updated_at`,
		req.Username, req.Email, string(hash), req.FullName,
	).Scan(&user.ID, &user.Username, &user.Email, &user.FullName, &user.AvatarURL, &user.Bio, &user.Location, &user.Website, &user.IsAdmin, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusConflict, "failed to create admin user")
		return
	}

	token, err := auth.GenerateToken(user.ID, user.Username, h.cfg.Secret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	writeJSON(w, http.StatusCreated, authResponse{Token: token, User: user})
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Username == "" || req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "username, email, and password are required")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	var user domain.User
	err = h.db.QueryRow(context.Background(),
		`INSERT INTO users (username, email, password, full_name)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, username, email, full_name, avatar_url, bio, location, website, is_admin, created_at, updated_at`,
		req.Username, req.Email, string(hash), req.FullName,
	).Scan(&user.ID, &user.Username, &user.Email, &user.FullName, &user.AvatarURL, &user.Bio, &user.Location, &user.Website, &user.IsAdmin, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusConflict, "username or email already exists")
		return
	}

	token, err := auth.GenerateToken(user.ID, user.Username, h.cfg.Secret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	writeJSON(w, http.StatusCreated, authResponse{Token: token, User: user})
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var user domain.User
	var hash string
	err := h.db.QueryRow(context.Background(),
		`SELECT id, username, email, password, full_name, avatar_url, bio, location, website, is_admin, created_at, updated_at
		 FROM users WHERE username = $1`, req.Username,
	).Scan(&user.ID, &user.Username, &user.Email, &hash, &user.FullName, &user.AvatarURL, &user.Bio, &user.Location, &user.Website, &user.IsAdmin, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	token, err := auth.GenerateToken(user.ID, user.Username, h.cfg.Secret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	writeJSON(w, http.StatusOK, authResponse{Token: token, User: user})
}

func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)

	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.OldPassword == "" || req.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "old_password and new_password are required")
		return
	}
	if len(req.NewPassword) < 6 {
		writeError(w, http.StatusBadRequest, "new password must be at least 6 characters")
		return
	}

	// Verify current password
	var hash string
	err := h.db.QueryRow(context.Background(),
		`SELECT password FROM users WHERE id = $1`, claims.UserID,
	).Scan(&hash)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.OldPassword)); err != nil {
		writeError(w, http.StatusUnauthorized, "current password is incorrect")
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	if _, err = h.db.Exec(context.Background(),
		`UPDATE users SET password = $1, updated_at = NOW() WHERE id = $2`,
		string(newHash), claims.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update password")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) GetCurrentUser(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	var user domain.User
	err := h.db.QueryRow(context.Background(),
		`SELECT id, username, email, full_name, avatar_url, bio, location, website, is_admin, created_at, updated_at
		 FROM users WHERE id = $1`, claims.UserID,
	).Scan(&user.ID, &user.Username, &user.Email, &user.FullName, &user.AvatarURL, &user.Bio, &user.Location, &user.Website, &user.IsAdmin, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, user)
}
