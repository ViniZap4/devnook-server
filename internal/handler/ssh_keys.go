package handler

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/gofiber/fiber/v2"
)

type sshKeyRequest struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

func (h *Handler) ListSSHKeys(c *fiber.Ctx) error {
	claims := getClaims(c)

	rows, err := h.db.Query(context.Background(),
		`SELECT id, user_id, name, fingerprint, content, created_at
		 FROM ssh_keys WHERE user_id = $1 ORDER BY created_at DESC`, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to list SSH keys")
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
	return writeJSON(c, fiber.StatusOK, keys)
}

func (h *Handler) CreateSSHKey(c *fiber.Ctx) error {
	claims := getClaims(c)

	var req sshKeyRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Name == "" || req.Content == "" {
		return writeError(c, fiber.StatusBadRequest, "name and content are required")
	}

	fingerprint := computeSSHFingerprint(req.Content)
	if fingerprint == "" {
		return writeError(c, fiber.StatusBadRequest, "invalid SSH key format")
	}

	var id int64
	err := h.db.QueryRow(context.Background(),
		`INSERT INTO ssh_keys (user_id, name, fingerprint, content) VALUES ($1, $2, $3, $4) RETURNING id`,
		claims.UserID, req.Name, fingerprint, strings.TrimSpace(req.Content),
	).Scan(&id)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to add SSH key")
	}
	return writeJSON(c, fiber.StatusCreated, map[string]any{"id": id, "fingerprint": fingerprint})
}

func (h *Handler) DeleteSSHKey(c *fiber.Ctx) error {
	claims := getClaims(c)
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid key id")
	}

	tag, err := h.db.Exec(context.Background(),
		`DELETE FROM ssh_keys WHERE id = $1 AND user_id = $2`, id, claims.UserID)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "SSH key not found")
	}
	return c.SendStatus(fiber.StatusNoContent)
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
