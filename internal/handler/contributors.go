package handler

import (
	"bufio"
	"net/http"
	"os/exec"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

type Contributor struct {
	Name    string `json:"name"`
	Email   string `json:"email"`
	Commits int    `json:"commits"`
}

// GetContributors returns contributor statistics for a repository.
func (h *Handler) GetContributors(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")
	repoDir := h.repoPath(owner, name)

	cmd := exec.Command("git", "-C", repoDir, "shortlog", "-sne", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		writeJSON(w, http.StatusOK, []Contributor{})
		return
	}

	var contributors []Contributor
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// Format: "  123\tAuthor Name <email@example.com>"
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}

		count, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
		nameEmail := parts[1]

		cName := nameEmail
		cEmail := ""
		if idx := strings.Index(nameEmail, " <"); idx > 0 {
			cName = nameEmail[:idx]
			cEmail = strings.Trim(nameEmail[idx+2:], ">")
		}

		contributors = append(contributors, Contributor{
			Name:    cName,
			Email:   cEmail,
			Commits: count,
		})
	}
	if contributors == nil {
		contributors = []Contributor{}
	}

	writeJSON(w, http.StatusOK, contributors)
}
