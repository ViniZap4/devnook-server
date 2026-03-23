package handler

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/gofiber/fiber/v2"
)

func (h *Handler) GitInfoRefs(c *fiber.Ctx) error {
	owner := c.Params("owner")
	repo := c.Params("repo")
	service := c.Query("service")

	if service != "git-upload-pack" && service != "git-receive-pack" {
		return c.Status(fiber.StatusBadRequest).SendString("invalid service")
	}

	repoPath := filepath.Join(h.cfg.ReposPath, owner, repo+".git")

	c.Set("Content-Type", fmt.Sprintf("application/x-%s-advertisement", service))
	c.Set("Cache-Control", "no-cache")

	// Write pkt-line header
	header := fmt.Sprintf("# service=%s\n", service)
	pktLine := fmt.Sprintf("%04x%s0000", len(header)+4, header)

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		w.WriteString(pktLine)
		cmd := exec.Command("git", service[4:], "--stateless-rpc", "--advertise-refs", repoPath)
		cmd.Stdout = w
		cmd.Run()
		w.Flush()
	})
	return nil
}

func (h *Handler) GitUploadPack(c *fiber.Ctx) error {
	return h.gitService(c, "upload-pack")
}

func (h *Handler) GitReceivePack(c *fiber.Ctx) error {
	return h.gitService(c, "receive-pack")
}

func (h *Handler) gitService(c *fiber.Ctx, service string) error {
	owner := c.Params("owner")
	repo := c.Params("repo")
	repoPath := filepath.Join(h.cfg.ReposPath, owner, repo+".git")

	c.Set("Content-Type", fmt.Sprintf("application/x-git-%s-result", service))

	body := c.Body()
	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		cmd := exec.Command("git", service, "--stateless-rpc", repoPath)
		cmd.Stdin = bytes.NewReader(body)
		cmd.Stdout = w
		cmd.Run()
		w.Flush()
	})
	return nil
}
