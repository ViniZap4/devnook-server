package handler

import (
	"net/http"
	"os/exec"
	"strings"

	"github.com/go-chi/chi/v5"
)

// GetArchive returns a zip or tar.gz archive of the repository at a given ref.
func (h *Handler) GetArchive(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "name")
	refFormat := chi.URLParam(r, "ref")

	repoDir := h.repoPath(owner, name)

	var ref, format string
	if strings.HasSuffix(refFormat, ".zip") {
		ref = strings.TrimSuffix(refFormat, ".zip")
		format = "zip"
	} else if strings.HasSuffix(refFormat, ".tar.gz") {
		ref = strings.TrimSuffix(refFormat, ".tar.gz")
		format = "tar.gz"
	} else {
		ref = refFormat
		format = "zip"
	}

	prefix := name + "-" + ref + "/"

	var cmd *exec.Cmd
	if format == "zip" {
		cmd = exec.Command("git", "-C", repoDir, "archive", "--format=zip", "--prefix="+prefix, ref)
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+name+"-"+ref+".zip\"")
	} else {
		cmd = exec.Command("git", "-C", repoDir, "archive", "--format=tar.gz", "--prefix="+prefix, ref)
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+name+"-"+ref+".tar.gz\"")
	}

	cmd.Stdout = w
	if err := cmd.Run(); err != nil {
		writeError(w, http.StatusNotFound, "archive not available")
		return
	}
}
