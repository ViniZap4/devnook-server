package handler

import (
	"net/http"
	"os/exec"

	"github.com/go-chi/chi/v5"
)

type createBranchRequest struct {
	Name string `json:"name"`
	From string `json:"from"` // source branch or ref
}

// CreateBranch creates a new branch in the repository.
func (h *Handler) CreateBranch(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")

	if owner != claims.Username {
		writeError(w, http.StatusForbidden, "not your repository")
		return
	}

	var req createBranchRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	from := req.From
	if from == "" {
		from = "HEAD"
	}

	repoDir := h.repoPath(owner, name)
	cmd := exec.Command("git", "-C", repoDir, "branch", req.Name, from)
	if out, err := cmd.CombinedOutput(); err != nil {
		writeError(w, http.StatusConflict, "failed to create branch: "+string(out))
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"name": req.Name})
}

// DeleteBranch deletes a branch from the repository.
func (h *Handler) DeleteBranch(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "name")
	branchName := chi.URLParam(r, "branch")

	if owner != claims.Username {
		writeError(w, http.StatusForbidden, "not your repository")
		return
	}

	repoDir := h.repoPath(owner, repoName)
	cmd := exec.Command("git", "-C", repoDir, "branch", "-D", branchName)
	if out, err := cmd.CombinedOutput(); err != nil {
		writeError(w, http.StatusBadRequest, "failed to delete branch: "+string(out))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
