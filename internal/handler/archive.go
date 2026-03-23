package handler

import (
	"bufio"
	"os/exec"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// GetArchive returns a zip or tar.gz archive of the repository at a given ref.
func (h *Handler) GetArchive(c *fiber.Ctx) error {
	owner := c.Params("owner")
	name := c.Params("name")
	refFormat := c.Params("ref")

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
	var contentType, disposition string
	if format == "zip" {
		cmd = exec.Command("git", "-C", repoDir, "archive", "--format=zip", "--prefix="+prefix, ref)
		contentType = "application/zip"
		disposition = "attachment; filename=\"" + name + "-" + ref + ".zip\""
	} else {
		cmd = exec.Command("git", "-C", repoDir, "archive", "--format=tar.gz", "--prefix="+prefix, ref)
		contentType = "application/gzip"
		disposition = "attachment; filename=\"" + name + "-" + ref + ".tar.gz\""
	}

	// Run command first to check for errors before streaming
	out, err := cmd.Output()
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "archive not available")
	}

	c.Set("Content-Type", contentType)
	c.Set("Content-Disposition", disposition)

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		w.Write(out)
		w.Flush()
	})
	return nil
}
