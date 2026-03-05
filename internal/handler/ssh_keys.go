package handler

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/go-chi/chi/v5"
)

type sshKeyRequest struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

func (h *Handler) ListSSHKeys(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)

	rows, err := h.db.Query(context.Background(),
		`SELECT id, user_id, name, fingerprint, content, created_at
		 FROM ssh_keys WHERE user_id = $1 ORDER BY created_at DESC`, claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list SSH keys")
		return
	}
	defer rows.Close()

	var keys []domain.SSHKey
	for rows.Next() {
		var k domain.SSHKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.Name, &k.Fingerprint, &k.Content, &k.CreatedAt); err != nil {
			continue
		}
		keys = append(keys, k)
	}
	if keys == nil {
		keys = []domain.SSHKey{}
	}
	writeJSON(w, http.StatusOK, keys)
}

func (h *Handler) CreateSSHKey(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)

	var req sshKeyRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || req.Content == "" {
		writeError(w, http.StatusBadRequest, "name and content are required")
		return
	}

	fingerprint := computeSSHFingerprint(req.Content)
	if fingerprint == "" {
		writeError(w, http.StatusBadRequest, "invalid SSH key format")
		return
	}

	var id int64
	err := h.db.QueryRow(context.Background(),
		`INSERT INTO ssh_keys (user_id, name, fingerprint, content) VALUES ($1, $2, $3, $4) RETURNING id`,
		claims.UserID, req.Name, fingerprint, strings.TrimSpace(req.Content),
	).Scan(&id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add SSH key")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "fingerprint": fingerprint})
}

func (h *Handler) DeleteSSHKey(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid key id")
		return
	}

	tag, err := h.db.Exec(context.Background(),
		`DELETE FROM ssh_keys WHERE id = $1 AND user_id = $2`, id, claims.UserID)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "SSH key not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func computeSSHFingerprint(pubkey string) string {
	parts := strings.Fields(strings.TrimSpace(pubkey))
	if len(parts) < 2 {
		return ""
	}
	decoded, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	hash := md5.Sum(decoded)
	var fp []string
	for _, b := range hash {
		fp = append(fp, fmt.Sprintf("%02x", b))
	}
	return strings.Join(fp, ":")
}
