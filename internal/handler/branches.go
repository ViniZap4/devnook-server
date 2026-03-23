package handler

import (
	"os/exec"

	"github.com/gofiber/fiber/v2"
)

type createBranchRequest struct {
	Name string `json:"name"`
	From string `json:"from"` // source branch or ref
}

// CreateBranch creates a new branch in the repository.
func (h *Handler) CreateBranch(c *fiber.Ctx) error {
	claims := getClaims(c)
	owner := c.Params("owner")
	name := c.Params("name")

	if owner != claims.Username {
		return writeError(c, fiber.StatusForbidden, "not your repository")
	}

	var req createBranchRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Name == "" {
		return writeError(c, fiber.StatusBadRequest, "name is required")
	}

	from := req.From
	if from == "" {
		from = "HEAD"
	}

	repoDir := h.repoPath(owner, name)
	cmd := exec.Command("git", "-C", repoDir, "branch", req.Name, from)
	if out, err := cmd.CombinedOutput(); err != nil {
		return writeError(c, fiber.StatusConflict, "failed to create branch: "+string(out))
	}

	return writeJSON(c, fiber.StatusCreated, map[string]string{"name": req.Name})
}

// DeleteBranch deletes a branch from the repository.
func (h *Handler) DeleteBranch(c *fiber.Ctx) error {
	claims := getClaims(c)
	owner := c.Params("owner")
	repoName := c.Params("name")
	branchName := c.Params("branch")

	if owner != claims.Username {
		return writeError(c, fiber.StatusForbidden, "not your repository")
	}

	repoDir := h.repoPath(owner, repoName)
	cmd := exec.Command("git", "-C", repoDir, "branch", "-D", branchName)
	if out, err := cmd.CombinedOutput(); err != nil {
		return writeError(c, fiber.StatusBadRequest, "failed to delete branch: "+string(out))
	}

	return c.SendStatus(fiber.StatusNoContent)
}
