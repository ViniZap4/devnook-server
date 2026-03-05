package handler

import (
	"bufio"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/go-chi/chi/v5"
)

// GetTags returns the list of tags with their hashes and dates.
func (h *Handler) GetTags(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")
	repoDir := h.repoPath(owner, name)

	format := "%(refname:short)%09%(objectname:short)%09%(*objectname:short)%09%(creatordate:iso8601)%09%(subject)"
	cmd := exec.Command("git", "-C", repoDir, "tag", "--sort=-creatordate", fmt.Sprintf("--format=%s", format))
	out, err := cmd.Output()
	if err != nil {
		writeJSON(w, http.StatusOK, []domain.Tag{})
		return
	}

	var tags []domain.Tag
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), "\t", 5)
		if len(parts) < 4 {
			continue
		}
		tag := domain.Tag{
			Name:    parts[0],
			Hash:    parts[1],
			Message: "",
		}
		// For annotated tags, use the dereferenced hash
		if parts[2] != "" {
			tag.Hash = parts[2]
		}
		if len(parts) >= 5 {
			tag.Message = parts[4]
		}
		tag.Date, _ = time.Parse("2006-01-02 15:04:05 -0700", parts[3])
		tags = append(tags, tag)
	}
	if tags == nil {
		tags = []domain.Tag{}
	}
	writeJSON(w, http.StatusOK, tags)
}

// DiffEntry represents a single file change in a diff.
type DiffEntry struct {
	OldPath string `json:"old_path"`
	NewPath string `json:"new_path"`
	Status  string `json:"status"`
	Patch   string `json:"patch"`
}

// CommitDetail represents a single commit with its diff.
type CommitDetail struct {
	domain.Commit
	Stats struct {
		Additions int `json:"additions"`
		Deletions int `json:"deletions"`
		Files     int `json:"files"`
	} `json:"stats"`
	Files []DiffEntry `json:"files"`
}

// GetCommitDetail returns a single commit with its diff.
func (h *Handler) GetCommitDetail(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")
	hash := chi.URLParam(r, "hash")
	repoDir := h.repoPath(owner, name)

	// Get commit info
	format := "%H%n%h%n%s%n%an%n%ae%n%aI"
	cmd := exec.Command("git", "-C", repoDir, "log", "-1", fmt.Sprintf("--format=%s", format), hash)
	out, err := cmd.Output()
	if err != nil {
		writeError(w, http.StatusNotFound, "commit not found")
		return
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 6 {
		writeError(w, http.StatusInternalServerError, "failed to parse commit")
		return
	}

	date, _ := time.Parse(time.RFC3339, lines[5])
	detail := CommitDetail{
		Commit: domain.Commit{
			Hash:      lines[0],
			ShortHash: lines[1],
			Message:   lines[2],
			Author:    lines[3],
			Email:     lines[4],
			Date:      date,
		},
	}

	// Get stats
	statsCmd := exec.Command("git", "-C", repoDir, "diff", "--stat", "--numstat", hash+"^.."+hash)
	statsOut, _ := statsCmd.Output()
	if statsOut != nil {
		scanner := bufio.NewScanner(strings.NewReader(string(statsOut)))
		for scanner.Scan() {
			parts := strings.Fields(scanner.Text())
			if len(parts) >= 3 {
				add := 0
				del := 0
				fmt.Sscanf(parts[0], "%d", &add)
				fmt.Sscanf(parts[1], "%d", &del)
				detail.Stats.Additions += add
				detail.Stats.Deletions += del
				detail.Stats.Files++
			}
		}
	}

	// Get diff
	diffCmd := exec.Command("git", "-C", repoDir, "diff", hash+"^.."+hash)
	diffOut, _ := diffCmd.Output()
	detail.Files = parseDiff(string(diffOut))

	writeJSON(w, http.StatusOK, detail)
}

// CompareBranches returns the diff between two refs.
func (h *Handler) CompareBranches(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")
	repoDir := h.repoPath(owner, name)

	base := r.URL.Query().Get("base")
	head := r.URL.Query().Get("head")
	if base == "" || head == "" {
		writeError(w, http.StatusBadRequest, "base and head parameters are required")
		return
	}

	type CompareResult struct {
		Commits []domain.Commit `json:"commits"`
		Files   []DiffEntry     `json:"files"`
		Stats   struct {
			Additions int `json:"additions"`
			Deletions int `json:"deletions"`
			Files     int `json:"files"`
		} `json:"stats"`
	}

	result := CompareResult{}

	// Get commits
	format := "%H%n%h%n%s%n%an%n%ae%n%aI"
	commitCmd := exec.Command("git", "-C", repoDir, "log", base+".."+head,
		fmt.Sprintf("--format=%s", format), "--max-count=100")
	commitOut, err := commitCmd.Output()
	if err != nil {
		writeError(w, http.StatusNotFound, "branches not found")
		return
	}

	commitLines := strings.Split(strings.TrimSpace(string(commitOut)), "\n")
	for i := 0; i+5 < len(commitLines); i += 6 {
		date, _ := time.Parse(time.RFC3339, commitLines[i+5])
		result.Commits = append(result.Commits, domain.Commit{
			Hash:      commitLines[i],
			ShortHash: commitLines[i+1],
			Message:   commitLines[i+2],
			Author:    commitLines[i+3],
			Email:     commitLines[i+4],
			Date:      date,
		})
	}
	if result.Commits == nil {
		result.Commits = []domain.Commit{}
	}

	// Get diff
	diffCmd := exec.Command("git", "-C", repoDir, "diff", base+"..."+head)
	diffOut, _ := diffCmd.Output()
	result.Files = parseDiff(string(diffOut))

	// Stats
	statsCmd := exec.Command("git", "-C", repoDir, "diff", "--numstat", base+"..."+head)
	statsOut, _ := statsCmd.Output()
	if statsOut != nil {
		scanner := bufio.NewScanner(strings.NewReader(string(statsOut)))
		for scanner.Scan() {
			parts := strings.Fields(scanner.Text())
			if len(parts) >= 3 {
				add := 0
				del := 0
				fmt.Sscanf(parts[0], "%d", &add)
				fmt.Sscanf(parts[1], "%d", &del)
				result.Stats.Additions += add
				result.Stats.Deletions += del
				result.Stats.Files++
			}
		}
	}

	if result.Files == nil {
		result.Files = []DiffEntry{}
	}

	writeJSON(w, http.StatusOK, result)
}

// GetBlame returns git blame output for a file.
func (h *Handler) GetBlame(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")
	ref := chi.URLParam(r, "ref")
	path := chi.URLParam(r, "*")
	repoDir := h.repoPath(owner, name)

	type BlameLine struct {
		Hash    string `json:"hash"`
		Author  string `json:"author"`
		Date    string `json:"date"`
		LineNum int    `json:"line_num"`
		Content string `json:"content"`
	}

	cmd := exec.Command("git", "-C", repoDir, "blame", "--porcelain", ref, "--", path)
	out, err := cmd.Output()
	if err != nil {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}

	var blameLines []BlameLine
	lines := strings.Split(string(out), "\n")
	currentHash := ""
	currentAuthor := ""
	currentDate := ""
	lineNum := 0

	for _, line := range lines {
		if len(line) >= 40 && line[0] != '\t' {
			parts := strings.Fields(line)
			if len(parts) >= 3 && len(parts[0]) == 40 {
				currentHash = parts[0][:7]
				fmt.Sscanf(parts[2], "%d", &lineNum)
			}
		}
		if strings.HasPrefix(line, "author ") {
			currentAuthor = strings.TrimPrefix(line, "author ")
		}
		if strings.HasPrefix(line, "author-time ") {
			ts := strings.TrimPrefix(line, "author-time ")
			var unix int64
			fmt.Sscanf(ts, "%d", &unix)
			currentDate = time.Unix(unix, 0).Format("2006-01-02")
		}
		if strings.HasPrefix(line, "\t") {
			blameLines = append(blameLines, BlameLine{
				Hash:    currentHash,
				Author:  currentAuthor,
				Date:    currentDate,
				LineNum: lineNum,
				Content: line[1:],
			})
		}
	}

	if blameLines == nil {
		blameLines = []BlameLine{}
	}
	writeJSON(w, http.StatusOK, blameLines)
}

// parseDiff parses unified diff output into DiffEntry slices.
func parseDiff(raw string) []DiffEntry {
	var entries []DiffEntry
	chunks := strings.Split(raw, "diff --git ")

	for _, chunk := range chunks[1:] {
		lines := strings.SplitN(chunk, "\n", -1)
		if len(lines) == 0 {
			continue
		}

		entry := DiffEntry{}
		// Parse file names from first line: "a/file b/file"
		parts := strings.SplitN(lines[0], " ", 2)
		if len(parts) == 2 {
			entry.OldPath = strings.TrimPrefix(parts[0], "a/")
			entry.NewPath = strings.TrimPrefix(parts[1], "b/")
		}

		// Determine status
		for _, line := range lines {
			if strings.HasPrefix(line, "new file") {
				entry.Status = "added"
				break
			}
			if strings.HasPrefix(line, "deleted file") {
				entry.Status = "deleted"
				break
			}
			if strings.HasPrefix(line, "rename from") {
				entry.Status = "renamed"
				break
			}
		}
		if entry.Status == "" {
			entry.Status = "modified"
		}

		// Collect patch (everything after the header)
		patchStart := -1
		for i, line := range lines {
			if strings.HasPrefix(line, "@@") {
				patchStart = i
				break
			}
		}
		if patchStart >= 0 {
			entry.Patch = strings.Join(lines[patchStart:], "\n")
		}

		entries = append(entries, entry)
	}
	return entries
}
