package handler

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gofiber/fiber/v2"
)

type createFileRequest struct {
	Content string `json:"content"`
	Message string `json:"message"`
	Branch  string `json:"branch"`
}

type updateFileRequest struct {
	Content string `json:"content"`
	Message string `json:"message"`
	Branch  string `json:"branch"`
}

type deleteFileRequest struct {
	Message string `json:"message"`
	Branch  string `json:"branch"`
}

// commitToRepo creates a temporary clone, makes changes, commits, and pushes.
func (h *Handler) commitToRepo(repoDir, branch, authorName, authorEmail, message string, fn func(workDir string) error) error {
	tmpDir, err := os.MkdirTemp("", "devnook-edit-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Clone
	cmd := exec.Command("git", "clone", "--branch", branch, "--single-branch", repoDir, tmpDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("clone failed: %s", string(out))
	}

	// Apply changes
	if err := fn(tmpDir); err != nil {
		return err
	}

	// Configure author
	exec.Command("git", "-C", tmpDir, "config", "user.name", authorName).Run()
	exec.Command("git", "-C", tmpDir, "config", "user.email", authorEmail).Run()

	// Stage all
	cmd = exec.Command("git", "-C", tmpDir, "add", "-A")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("stage failed: %s", string(out))
	}

	// Check if there are changes
	cmd = exec.Command("git", "-C", tmpDir, "diff", "--cached", "--quiet")
	if cmd.Run() == nil {
		return fmt.Errorf("no changes to commit")
	}

	// Commit
	cmd = exec.Command("git", "-C", tmpDir, "commit", "-m", message)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("commit failed: %s", string(out))
	}

	// Push
	cmd = exec.Command("git", "-C", tmpDir, "push", "origin", branch)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("push failed: %s", string(out))
	}

	return nil
}

// CreateFile creates a new file via the API.
func (h *Handler) CreateFile(c *fiber.Ctx) error {
	claims := getClaims(c)
	owner := c.Params("owner")
	name := c.Params("name")
	path := c.Params("*")

	// Verify caller has write access
	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "repository not found")
	}
	var repoOwnerID int64
	h.db.QueryRow(context.Background(),
		`SELECT owner_id FROM repositories WHERE id = $1`, repoID).Scan(&repoOwnerID)
	if claims.UserID != repoOwnerID {
		// Check if collaborator with write access
		var perm string
		err := h.db.QueryRow(context.Background(),
			`SELECT permission FROM repo_collaborators WHERE repo_id = $1 AND user_id = $2`,
			repoID, claims.UserID).Scan(&perm)
		if err != nil || (perm != "write" && perm != "admin") {
			return writeError(c, fiber.StatusForbidden, "you don't have write access to this repository")
		}
	}

	var req createFileRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}

	branch := req.Branch
	if branch == "" {
		branch = "main"
	}
	message := req.Message
	if message == "" {
		message = "Create " + path
	}

	repoDir := h.repoPath(owner, name)

	var authorName, authorEmail string
	h.db.QueryRow(context.Background(),
		`SELECT COALESCE(full_name, username), email FROM users WHERE id = $1`,
		claims.UserID).Scan(&authorName, &authorEmail)

	err = h.commitToRepo(repoDir, branch, authorName, authorEmail, message, func(workDir string) error {
		filePath := filepath.Join(workDir, path)
		if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}
		if _, err := os.Stat(filePath); err == nil {
			return fmt.Errorf("file already exists")
		}
		return os.WriteFile(filePath, []byte(req.Content), 0o644)
	})
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			return writeError(c, fiber.StatusConflict, "file already exists")
		}
		return writeError(c, fiber.StatusInternalServerError, err.Error())
	}

	return writeJSON(c, fiber.StatusCreated, map[string]string{"message": "file created"})
}

// UpdateFile updates an existing file via the API.
func (h *Handler) UpdateFile(c *fiber.Ctx) error {
	claims := getClaims(c)
	owner := c.Params("owner")
	name := c.Params("name")
	path := c.Params("*")

	// Verify caller has write access
	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "repository not found")
	}
	var repoOwnerID int64
	h.db.QueryRow(context.Background(),
		`SELECT owner_id FROM repositories WHERE id = $1`, repoID).Scan(&repoOwnerID)
	if claims.UserID != repoOwnerID {
		// Check if collaborator with write access
		var perm string
		err := h.db.QueryRow(context.Background(),
			`SELECT permission FROM repo_collaborators WHERE repo_id = $1 AND user_id = $2`,
			repoID, claims.UserID).Scan(&perm)
		if err != nil || (perm != "write" && perm != "admin") {
			return writeError(c, fiber.StatusForbidden, "you don't have write access to this repository")
		}
	}

	var req updateFileRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}

	branch := req.Branch
	if branch == "" {
		branch = "main"
	}
	message := req.Message
	if message == "" {
		message = "Update " + path
	}

	repoDir := h.repoPath(owner, name)

	var authorName, authorEmail string
	h.db.QueryRow(context.Background(),
		`SELECT COALESCE(full_name, username), email FROM users WHERE id = $1`,
		claims.UserID).Scan(&authorName, &authorEmail)

	err = h.commitToRepo(repoDir, branch, authorName, authorEmail, message, func(workDir string) error {
		filePath := filepath.Join(workDir, path)
		return os.WriteFile(filePath, []byte(req.Content), 0o644)
	})
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, err.Error())
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// DeleteFile deletes a file via the API.
func (h *Handler) DeleteFile(c *fiber.Ctx) error {
	claims := getClaims(c)
	owner := c.Params("owner")
	name := c.Params("name")
	path := c.Params("*")

	// Verify caller has write access
	repoID, err := h.getRepoID(owner, name)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "repository not found")
	}
	var repoOwnerID int64
	h.db.QueryRow(context.Background(),
		`SELECT owner_id FROM repositories WHERE id = $1`, repoID).Scan(&repoOwnerID)
	if claims.UserID != repoOwnerID {
		// Check if collaborator with write access
		var perm string
		err := h.db.QueryRow(context.Background(),
			`SELECT permission FROM repo_collaborators WHERE repo_id = $1 AND user_id = $2`,
			repoID, claims.UserID).Scan(&perm)
		if err != nil || (perm != "write" && perm != "admin") {
			return writeError(c, fiber.StatusForbidden, "you don't have write access to this repository")
		}
	}

	var req deleteFileRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}

	branch := req.Branch
	if branch == "" {
		branch = "main"
	}
	message := req.Message
	if message == "" {
		message = "Delete " + path
	}

	repoDir := h.repoPath(owner, name)

	var authorName, authorEmail string
	h.db.QueryRow(context.Background(),
		`SELECT COALESCE(full_name, username), email FROM users WHERE id = $1`,
		claims.UserID).Scan(&authorName, &authorEmail)

	err = h.commitToRepo(repoDir, branch, authorName, authorEmail, message, func(workDir string) error {
		filePath := filepath.Join(workDir, path)
		return os.Remove(filePath)
	})
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, err.Error())
	}

	return c.SendStatus(fiber.StatusNoContent)
}
