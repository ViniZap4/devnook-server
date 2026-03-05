package handler

import (
	"bufio"
	"net/http"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Language extension mapping
var langExtensions = map[string]string{
	".go":     "Go",
	".rs":     "Rust",
	".py":     "Python",
	".js":     "JavaScript",
	".ts":     "TypeScript",
	".jsx":    "JavaScript",
	".tsx":    "TypeScript",
	".java":   "Java",
	".kt":     "Kotlin",
	".c":      "C",
	".h":      "C",
	".cpp":    "C++",
	".hpp":    "C++",
	".cc":     "C++",
	".cs":     "C#",
	".rb":     "Ruby",
	".php":    "PHP",
	".swift":  "Swift",
	".m":      "Objective-C",
	".lua":    "Lua",
	".r":      "R",
	".scala":  "Scala",
	".zig":    "Zig",
	".dart":   "Dart",
	".ex":     "Elixir",
	".exs":    "Elixir",
	".erl":    "Erlang",
	".hs":     "Haskell",
	".ml":     "OCaml",
	".clj":    "Clojure",
	".svelte": "Svelte",
	".vue":    "Vue",
	".html":   "HTML",
	".css":    "CSS",
	".scss":   "SCSS",
	".less":   "Less",
	".json":   "JSON",
	".yaml":   "YAML",
	".yml":    "YAML",
	".toml":   "TOML",
	".xml":    "XML",
	".sql":    "SQL",
	".sh":     "Shell",
	".bash":   "Shell",
	".zsh":    "Shell",
	".ps1":    "PowerShell",
	".md":     "Markdown",
	".txt":    "Text",
	".makefile": "Makefile",
	".dockerfile": "Dockerfile",
	".tf":     "Terraform",
	".nim":    "Nim",
	".v":      "V",
}

// Language color mapping
var langColors = map[string]string{
	"Go":          "#00ADD8",
	"Rust":        "#DEA584",
	"Python":      "#3572A5",
	"JavaScript":  "#F1E05A",
	"TypeScript":  "#3178C6",
	"Java":        "#B07219",
	"Kotlin":      "#A97BFF",
	"C":           "#555555",
	"C++":         "#F34B7D",
	"C#":          "#178600",
	"Ruby":        "#701516",
	"PHP":         "#4F5D95",
	"Swift":       "#F05138",
	"Lua":         "#000080",
	"Dart":        "#00B4AB",
	"Elixir":      "#6E4A7E",
	"Haskell":     "#5E5086",
	"Svelte":      "#FF3E00",
	"Vue":         "#41B883",
	"HTML":        "#E34C26",
	"CSS":         "#563D7C",
	"SCSS":        "#C6538C",
	"Shell":       "#89E051",
	"Markdown":    "#083FA1",
	"SQL":         "#E38C00",
	"Zig":         "#EC915C",
	"Nim":         "#FFC200",
}

type LanguageStat struct {
	Name       string  `json:"name"`
	Bytes      int64   `json:"bytes"`
	Percentage float64 `json:"percentage"`
	Color      string  `json:"color"`
}

// GetLanguages returns language statistics for a repository.
func (h *Handler) GetLanguages(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")
	repoDir := h.repoPath(owner, name)

	// Use git ls-tree to list all files with sizes
	cmd := exec.Command("git", "-C", repoDir, "ls-tree", "-r", "-l", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		writeJSON(w, http.StatusOK, []LanguageStat{})
		return
	}

	langBytes := make(map[string]int64)
	var totalBytes int64

	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		tabIdx := strings.Index(line, "\t")
		if tabIdx < 0 {
			continue
		}
		meta := strings.Fields(line[:tabIdx])
		fileName := line[tabIdx+1:]

		if len(meta) < 4 || meta[1] != "blob" {
			continue
		}

		size, _ := strconv.ParseInt(strings.TrimSpace(meta[3]), 10, 64)
		if size == 0 {
			continue
		}

		ext := strings.ToLower(filepath.Ext(fileName))
		baseName := strings.ToLower(filepath.Base(fileName))

		var lang string
		// Check special filenames
		switch baseName {
		case "makefile":
			lang = "Makefile"
		case "dockerfile":
			lang = "Dockerfile"
		case "cmakelists.txt":
			lang = "CMake"
		default:
			lang = langExtensions[ext]
		}

		if lang == "" {
			continue
		}

		langBytes[lang] += size
		totalBytes += size
	}

	if totalBytes == 0 {
		writeJSON(w, http.StatusOK, []LanguageStat{})
		return
	}

	var stats []LanguageStat
	for lang, bytes := range langBytes {
		pct := float64(bytes) / float64(totalBytes) * 100
		color := langColors[lang]
		if color == "" {
			color = "#8b8b8b"
		}
		stats = append(stats, LanguageStat{
			Name:       lang,
			Bytes:      bytes,
			Percentage: pct,
			Color:      color,
		})
	}

	// Sort by bytes descending
	for i := 0; i < len(stats); i++ {
		for j := i + 1; j < len(stats); j++ {
			if stats[j].Bytes > stats[i].Bytes {
				stats[i], stats[j] = stats[j], stats[i]
			}
		}
	}

	writeJSON(w, http.StatusOK, stats)
}
