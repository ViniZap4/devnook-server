package handler

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/gofiber/fiber/v2"
)

type createOrgRequest struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
}

type updateOrgRequest struct {
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
}

type addMemberRequest struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

func (h *Handler) CreateOrg(c *fiber.Ctx) error {
	claims := getClaims(c)
	var req createOrgRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return writeError(c, fiber.StatusBadRequest, "name is required")
	}

	// Check name doesn't collide with usernames
	var exists bool
	h.db.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM users WHERE username = $1)`, req.Name,
	).Scan(&exists)
	if exists {
		return writeError(c, fiber.StatusConflict, "name already taken by a user")
	}

	ctx := context.Background()
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to begin transaction")
	}
	defer tx.Rollback(ctx)

	var orgID int64
	err = tx.QueryRow(ctx,
		`INSERT INTO organizations (name, display_name, description)
		 VALUES ($1, $2, $3) RETURNING id`,
		req.Name, req.DisplayName, req.Description,
	).Scan(&orgID)
	if err != nil {
		return writeError(c, fiber.StatusConflict, "organization name already exists")
	}

	// Creator becomes owner
	_, err = tx.Exec(ctx,
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'owner')`,
		orgID, claims.UserID,
	)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to add owner")
	}

	if err := tx.Commit(ctx); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to commit")
	}

	return writeJSON(c, fiber.StatusCreated, map[string]interface{}{"id": orgID, "name": req.Name})
}

func (h *Handler) ListOrgs(c *fiber.Ctx) error {
	claims := getClaims(c)
	rows, err := h.db.Query(context.Background(),
		`SELECT o.id, o.name, o.display_name, o.description, o.avatar_url, o.location, o.website, o.created_at, o.updated_at
		 FROM organizations o
		 JOIN org_members m ON m.org_id = o.id
		 WHERE m.user_id = $1
		 ORDER BY o.name`, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to list orgs")
	}
	defer rows.Close()

	var orgs []domain.Organization
	for rows.Next() {
		var o domain.Organization
		if err := rows.Scan(&o.ID, &o.Name, &o.DisplayName, &o.Description, &o.AvatarURL, &o.Location, &o.Website, &o.CreatedAt, &o.UpdatedAt); err != nil {
			continue
		}
		orgs = append(orgs, o)
	}
	if orgs == nil {
		orgs = []domain.Organization{}
	}
	return writeJSON(c, fiber.StatusOK, orgs)
}

func (h *Handler) GetOrg(c *fiber.Ctx) error {
	name := c.Params("name")
	var o domain.Organization
	err := h.db.QueryRow(context.Background(),
		`SELECT id, name, display_name, description, avatar_url, location, website, created_at, updated_at
		 FROM organizations WHERE name = $1`, name,
	).Scan(&o.ID, &o.Name, &o.DisplayName, &o.Description, &o.AvatarURL, &o.Location, &o.Website, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "organization not found")
	}
	return writeJSON(c, fiber.StatusOK, o)
}

func (h *Handler) UpdateOrg(c *fiber.Ctx) error {
	claims := getClaims(c)
	name := c.Params("name")

	if !h.isOrgOwner(claims.UserID, name) {
		return writeError(c, fiber.StatusForbidden, "not an owner of this organization")
	}

	var req updateOrgRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}

	tag, err := h.db.Exec(context.Background(),
		`UPDATE organizations SET display_name=$1, description=$2, updated_at=NOW()
		 WHERE name=$3`,
		req.DisplayName, req.Description, name)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "organization not found")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) DeleteOrg(c *fiber.Ctx) error {
	claims := getClaims(c)
	name := c.Params("name")

	if !h.isOrgOwner(claims.UserID, name) {
		return writeError(c, fiber.StatusForbidden, "not an owner of this organization")
	}

	tag, err := h.db.Exec(context.Background(),
		`DELETE FROM organizations WHERE name = $1`, name)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "organization not found")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) ListOrgMembers(c *fiber.Ctx) error {
	name := c.Params("name")
	rows, err := h.db.Query(context.Background(),
		`SELECT m.id, m.org_id, m.user_id, u.username, u.full_name, m.role, m.joined_at
		 FROM org_members m
		 JOIN users u ON u.id = m.user_id
		 JOIN organizations o ON o.id = m.org_id
		 WHERE o.name = $1
		 ORDER BY m.joined_at`, name)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to list members")
	}
	defer rows.Close()

	var members []domain.OrgMember
	for rows.Next() {
		var m domain.OrgMember
		if err := rows.Scan(&m.ID, &m.OrgID, &m.UserID, &m.Username, &m.FullName, &m.Role, &m.JoinedAt); err != nil {
			continue
		}
		members = append(members, m)
	}
	if members == nil {
		members = []domain.OrgMember{}
	}
	return writeJSON(c, fiber.StatusOK, members)
}

func (h *Handler) AddOrgMember(c *fiber.Ctx) error {
	claims := getClaims(c)
	name := c.Params("name")

	if !h.isOrgOwner(claims.UserID, name) {
		return writeError(c, fiber.StatusForbidden, "not an owner of this organization")
	}

	var req addMemberRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Role == "" {
		req.Role = "member"
	}

	var userID int64
	err := h.db.QueryRow(context.Background(),
		`SELECT id FROM users WHERE username = $1`, req.Username,
	).Scan(&userID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "user not found")
	}

	var orgID int64
	err = h.db.QueryRow(context.Background(),
		`SELECT id FROM organizations WHERE name = $1`, name,
	).Scan(&orgID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "organization not found")
	}

	_, err = h.db.Exec(context.Background(),
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, $3)
		 ON CONFLICT (org_id, user_id) DO UPDATE SET role = $3`,
		orgID, userID, req.Role,
	)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to add member")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) UpdateOrgMember(c *fiber.Ctx) error {
	claims := getClaims(c)
	orgName := c.Params("name")
	username := c.Params("username")

	if !h.isOrgOwner(claims.UserID, orgName) {
		return writeError(c, fiber.StatusForbidden, "not an owner of this organization")
	}

	var req struct {
		Role string `json:"role"`
	}
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}

	tag, err := h.db.Exec(context.Background(),
		`UPDATE org_members SET role = $1
		 WHERE user_id = (SELECT id FROM users WHERE username = $2)
		 AND org_id = (SELECT id FROM organizations WHERE name = $3)`,
		req.Role, username, orgName,
	)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "member not found")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) RemoveOrgMember(c *fiber.Ctx) error {
	claims := getClaims(c)
	orgName := c.Params("name")
	username := c.Params("username")

	if !h.isOrgOwner(claims.UserID, orgName) {
		return writeError(c, fiber.StatusForbidden, "not an owner of this organization")
	}

	tag, err := h.db.Exec(context.Background(),
		`DELETE FROM org_members
		 WHERE user_id = (SELECT id FROM users WHERE username = $1)
		 AND org_id = (SELECT id FROM organizations WHERE name = $2)`,
		username, orgName,
	)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "member not found")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) ListOrgRepos(c *fiber.Ctx) error {
	name := c.Params("name")

	rows, err := h.db.Query(context.Background(),
		`SELECT r.id, r.owner_id, o.name, r.name, r.description, r.is_private, r.default_branch, r.org_id, r.created_at, r.updated_at
		 FROM repositories r
		 JOIN organizations o ON o.id = r.org_id
		 WHERE o.name = $1
		 ORDER BY r.updated_at DESC`, name)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to list repos")
	}
	defer rows.Close()

	var repos []domain.Repository
	for rows.Next() {
		var repo domain.Repository
		if err := rows.Scan(&repo.ID, &repo.OwnerID, &repo.Owner, &repo.Name, &repo.Description,
			&repo.IsPrivate, &repo.DefaultBranch, &repo.OrgID, &repo.CreatedAt, &repo.UpdatedAt); err != nil {
			continue
		}
		repos = append(repos, repo)
	}
	if repos == nil {
		repos = []domain.Repository{}
	}
	return writeJSON(c, fiber.StatusOK, repos)
}

func (h *Handler) CreateOrgRepo(c *fiber.Ctx) error {
	claims := getClaims(c)
	orgName := c.Params("name")

	// Check membership
	var orgID int64
	err := h.db.QueryRow(context.Background(),
		`SELECT o.id FROM organizations o
		 JOIN org_members m ON m.org_id = o.id
		 WHERE o.name = $1 AND m.user_id = $2`, orgName, claims.UserID,
	).Scan(&orgID)
	if err != nil {
		return writeError(c, fiber.StatusForbidden, "not a member of this organization")
	}

	var req createRepoRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Name == "" {
		return writeError(c, fiber.StatusBadRequest, "name is required")
	}

	var repoID int64
	err = h.db.QueryRow(context.Background(),
		`INSERT INTO repositories (owner_id, name, description, is_private, org_id)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		claims.UserID, req.Name, req.Description, req.IsPrivate, orgID,
	).Scan(&repoID)
	if err != nil {
		return writeError(c, fiber.StatusConflict, "repository already exists")
	}

	// Initialize bare git repo under org name
	repoPath := filepath.Join(h.cfg.ReposPath, orgName, req.Name+".git")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to create repo directory")
	}
	cmd := exec.Command("git", "init", "--bare", repoPath)
	if err := cmd.Run(); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to initialize git repo")
	}

	return writeJSON(c, fiber.StatusCreated, map[string]interface{}{"id": repoID, "name": req.Name})
}

func (h *Handler) isOrgOwner(userID int64, orgName string) bool {
	var role string
	err := h.db.QueryRow(context.Background(),
		`SELECT m.role FROM org_members m
		 JOIN organizations o ON o.id = m.org_id
		 WHERE o.name = $1 AND m.user_id = $2`, orgName, userID,
	).Scan(&role)
	return err == nil && role == "owner"
}
