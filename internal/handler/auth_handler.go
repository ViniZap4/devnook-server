package handler

import (
	"context"
	"net/http"

	"github.com/ViniZap4/devnook-server/internal/auth"
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

type tokenResponse struct {
	Token string `json:"token"`
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

	var userID int64
	err = h.db.QueryRow(context.Background(),
		`INSERT INTO users (username, email, password, full_name)
		 VALUES ($1, $2, $3, $4) RETURNING id`,
		req.Username, req.Email, string(hash), req.FullName,
	).Scan(&userID)
	if err != nil {
		writeError(w, http.StatusConflict, "username or email already exists")
		return
	}

	token, err := auth.GenerateToken(userID, req.Username, h.cfg.Secret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	writeJSON(w, http.StatusCreated, tokenResponse{Token: token})
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var userID int64
	var hash string
	err := h.db.QueryRow(context.Background(),
		`SELECT id, password FROM users WHERE username = $1`, req.Username,
	).Scan(&userID, &hash)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	token, err := auth.GenerateToken(userID, req.Username, h.cfg.Secret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	writeJSON(w, http.StatusOK, tokenResponse{Token: token})
}

func (h *Handler) GetCurrentUser(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	var user struct {
		ID        int64  `json:"id"`
		Username  string `json:"username"`
		Email     string `json:"email"`
		FullName  string `json:"full_name"`
		AvatarURL string `json:"avatar_url"`
	}
	err := h.db.QueryRow(context.Background(),
		`SELECT id, username, email, full_name, avatar_url FROM users WHERE id = $1`,
		claims.UserID,
	).Scan(&user.ID, &user.Username, &user.Email, &user.FullName, &user.AvatarURL)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, user)
}
