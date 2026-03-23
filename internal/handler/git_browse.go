package handler

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/gofiber/fiber/v2"
)

// repoPath returns the bare repo path on disk for the given owner/name.
func (h *Handler) repoPath(owner, name string) string {
	return filepath.Join(h.cfg.ReposPath, owner, name+".git")
}

// resolveRepoPath looks up the repo and returns (repoPath, error).
// Owner can be a user or org name.
func (h *Handler) resolveRepoPath(c *fiber.Ctx) (string, error) {
	owner := c.Params("owner")
	name := c.Params("name")
	return h.repoPath(owner, name), nil
}

// GetTree returns the directory listing at a given ref and path.
func (h *Handler) GetTree(c *fiber.Ctx) error {
	owner := c.Params("owner")
	name := c.Params("name")
	ref := c.Params("ref")
	path := c.Params("*")

	repoDir := h.repoPath(owner, name)

	treeish := ref
	if path != "" {
		treeish = ref + ":" + path
	}

	cmd := exec.Command("git", "-C", repoDir, "ls-tree", "-l", treeish)
	out, err := cmd.Output()
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "tree not found")
	}

	var entries []domain.TreeEntry
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		// format: <mode> <type> <hash> <size>\t<name>
		tabIdx := strings.Index(line, "\t")
		if tabIdx < 0 {
			continue
		}
		meta := strings.Fields(line[:tabIdx])
		entryName := line[tabIdx+1:]

		if len(meta) < 4 {
			continue
		}

		entry := domain.TreeEntry{
			Mode: meta[0],
			Type: meta[1],
			Name: entryName,
		}
		if path != "" {
			entry.Path = path + "/" + entryName
		} else {
			entry.Path = entryName
		}
		if meta[3] != "-" {
			entry.Size, _ = strconv.ParseInt(meta[3], 10, 64)
		}

		entries = append(entries, entry)
	}
	if entries == nil {
		entries = []domain.TreeEntry{}
	}

	return writeJSON(c, fiber.StatusOK, entries)
}

// GetBlob returns file content at a given ref and path.
func (h *Handler) GetBlob(c *fiber.Ctx) error {
	owner := c.Params("owner")
	name := c.Params("name")
	ref := c.Params("ref")
	path := c.Params("*")

	repoDir := h.repoPath(owner, name)

	// Get file size
	sizeCmd := exec.Command("git", "-C", repoDir, "cat-file", "-s", ref+":"+path)
	sizeOut, err := sizeCmd.Output()
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "file not found")
	}
	size, _ := strconv.ParseInt(strings.TrimSpace(string(sizeOut)), 10, 64)

	// Get file content
	cmd := exec.Command("git", "-C", repoDir, "show", ref+":"+path)
	out, err := cmd.Output()
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "file not found")
	}

	binary := !utf8.Valid(out)
	content := ""
	if !binary {
		content = string(out)
	}

	blob := domain.BlobContent{
		Name:    filepath.Base(path),
		Path:    path,
		Size:    size,
		Content: content,
		Binary:  binary,
	}

	return writeJSON(c, fiber.StatusOK, blob)
}

// GetCommits returns the commit history for a ref.
func (h *Handler) GetCommits(c *fiber.Ctx) error {
	owner := c.Params("owner")
	name := c.Params("name")
	repoDir := h.repoPath(owner, name)

	ref := c.Query("ref")
	if ref == "" {
		ref = "HEAD"
	}

	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	perPage := 30
	skip := (page - 1) * perPage

	format := "%H%n%h%n%s%n%an%n%ae%n%aI"
	cmd := exec.Command("git", "-C", repoDir, "log", ref,
		fmt.Sprintf("--format=%s", format),
		fmt.Sprintf("--skip=%d", skip),
		fmt.Sprintf("--max-count=%d", perPage),
	)
	out, err := cmd.Output()
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "commits not found")
	}

	var commits []domain.Commit
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for i := 0; i+5 < len(lines); i += 6 {
		date, _ := time.Parse(time.RFC3339, lines[i+5])
		commits = append(commits, domain.Commit{
			Hash:      lines[i],
			ShortHash: lines[i+1],
			Message:   lines[i+2],
			Author:    lines[i+3],
			Email:     lines[i+4],
			Date:      date,
		})
	}
	if commits == nil {
		commits = []domain.Commit{}
	}

	return writeJSON(c, fiber.StatusOK, commits)
}

// GetBranches returns the list of branches.
func (h *Handler) GetBranches(c *fiber.Ctx) error {
	owner := c.Params("owner")
	name := c.Params("name")
	repoDir := h.repoPath(owner, name)

	// Get default branch from DB
	var defaultBranch string
	err := h.db.QueryRow(context.Background(),
		`SELECT r.default_branch FROM repositories r
		 JOIN users u ON r.owner_id = u.id
		 WHERE u.username = $1 AND r.name = $2`, owner, name,
	).Scan(&defaultBranch)
	if err != nil {
		defaultBranch = "main"
	}

	cmd := exec.Command("git", "-C", repoDir, "branch", "--format=%(refname:short)%(HEAD)")
	out, err := cmd.Output()
	if err != nil {
		return writeJSON(c, fiber.StatusOK, []domain.Branch{})
	}

	var branches []domain.Branch
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		isHead := strings.HasSuffix(line, "*")
		branchName := strings.TrimSuffix(line, "*")
		branchName = strings.TrimSpace(branchName)
		if branchName == "" {
			continue
		}
		branches = append(branches, domain.Branch{
			Name:      branchName,
			IsDefault: branchName == defaultBranch,
			IsHead:    isHead,
		})
	}
	if branches == nil {
		branches = []domain.Branch{}
	}

	return writeJSON(c, fiber.StatusOK, branches)
}

// GetReadme tries to find and return a README file from the repo root.
func (h *Handler) GetReadme(c *fiber.Ctx) error {
	owner := c.Params("owner")
	name := c.Params("name")
	repoDir := h.repoPath(owner, name)

	ref := c.Query("ref")
	if ref == "" {
		ref = "HEAD"
	}

	candidates := []string{"README.md", "readme.md", "README", "README.txt", "Readme.md"}

	for _, candidate := range candidates {
		cmd := exec.Command("git", "-C", repoDir, "show", ref+":"+candidate)
		out, err := cmd.Output()
		if err == nil {
			return writeJSON(c, fiber.StatusOK, map[string]string{
				"name":    candidate,
				"content": string(out),
			})
		}
	}

	return writeError(c, fiber.StatusNotFound, "no readme found")
}
