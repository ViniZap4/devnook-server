package handler

import (
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) GitInfoRefs(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	service := r.URL.Query().Get("service")

	if service != "git-upload-pack" && service != "git-receive-pack" {
		http.Error(w, "invalid service", http.StatusBadRequest)
		return
	}

	repoPath := filepath.Join(h.cfg.ReposPath, owner, repo+".git")

	w.Header().Set("Content-Type", fmt.Sprintf("application/x-%s-advertisement", service))
	w.Header().Set("Cache-Control", "no-cache")

	// Write pkt-line header
	header := fmt.Sprintf("# service=%s\n", service)
	pktLine := fmt.Sprintf("%04x%s0000", len(header)+4, header)
	w.Write([]byte(pktLine))

	cmd := exec.Command("git", service[4:], "--stateless-rpc", "--advertise-refs", repoPath)
	cmd.Stdout = w
	cmd.Run()
}

func (h *Handler) GitUploadPack(w http.ResponseWriter, r *http.Request) {
	h.gitService(w, r, "upload-pack")
}

func (h *Handler) GitReceivePack(w http.ResponseWriter, r *http.Request) {
	h.gitService(w, r, "receive-pack")
}

func (h *Handler) gitService(w http.ResponseWriter, r *http.Request, service string) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	repoPath := filepath.Join(h.cfg.ReposPath, owner, repo+".git")

	w.Header().Set("Content-Type", fmt.Sprintf("application/x-git-%s-result", service))

	cmd := exec.Command("git", service, "--stateless-rpc", repoPath)
	cmd.Stdin = r.Body
	cmd.Stdout = w
	cmd.Run()
}
