package handler

import (
	"context"
	"strconv"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/gofiber/fiber/v2"
)

// getProjectBySlug resolves a project slug to its ID, verifying the caller is a member.
func (h *Handler) getProjectBySlug(ctx context.Context, slug string, userID int64) (int64, error) {
	var projectID int64
	err := h.db.QueryRow(ctx,
		`SELECT p.id FROM projects p
		 JOIN project_members pm ON pm.project_id = p.id
		 WHERE p.slug = $1 AND pm.user_id = $2`, slug, userID).Scan(&projectID)
	return projectID, err
}

// getProjectRole returns the caller's role in a project, or an error if not a member.
func (h *Handler) getProjectRole(ctx context.Context, projectID, userID int64) (string, error) {
	var role string
	err := h.db.QueryRow(ctx,
		`SELECT role FROM project_members WHERE project_id = $1 AND user_id = $2`,
		projectID, userID).Scan(&role)
	return role, err
}

// --- Projects ---

func (h *Handler) ListProjects(c *fiber.Ctx) error {
	claims := getClaims(c)
	rows, err := h.db.Query(context.Background(),
		`SELECT p.id, p.owner_id, u.username, p.org_id,
		        (SELECT o.name FROM organizations o WHERE o.id = p.org_id),
		        p.name, p.slug, p.description, p.methodology, p.visibility,
		        p.default_view, p.color, p.icon,
		        (SELECT COUNT(*) FROM project_members pm2 WHERE pm2.project_id = p.id),
		        (SELECT COUNT(*) FROM project_items pi WHERE pi.project_id = p.id),
		        p.created_at, p.updated_at
		 FROM projects p
		 JOIN project_members pm ON pm.project_id = p.id
		 JOIN users u ON u.id = p.owner_id
		 WHERE pm.user_id = $1
		 ORDER BY p.updated_at DESC`, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to list projects")
	}
	defer rows.Close()

	var projects []domain.Project
	for rows.Next() {
		var p domain.Project
		if err := rows.Scan(
			&p.ID, &p.OwnerID, &p.OwnerName, &p.OrgID, &p.OrgName,
			&p.Name, &p.Slug, &p.Description, &p.Methodology, &p.Visibility,
			&p.DefaultView, &p.Color, &p.Icon,
			&p.MemberCount, &p.ItemCount,
			&p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			continue
		}
		projects = append(projects, p)
	}
	if projects == nil {
		projects = []domain.Project{}
	}
	return writeJSON(c, fiber.StatusOK, projects)
}

type createProjectRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Methodology string `json:"methodology"`
	Visibility  string `json:"visibility"`
	DefaultView string `json:"default_view"`
	Color       string `json:"color"`
	Icon        string `json:"icon"`
}

func (h *Handler) CreateProject(c *fiber.Ctx) error {
	claims := getClaims(c)
	var req createProjectRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Name == "" {
		return writeError(c, fiber.StatusBadRequest, "name is required")
	}

	methodology := req.Methodology
	if methodology == "" {
		methodology = "kanban"
	}
	visibility := req.Visibility
	if visibility == "" {
		visibility = "private"
	}
	defaultView := req.DefaultView
	if defaultView == "" {
		defaultView = "board"
	}

	slug := toSlug(req.Name)

	ctx := context.Background()
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to begin transaction")
	}
	defer tx.Rollback(ctx)

	var projectID int64
	var resultSlug string
	err = tx.QueryRow(ctx,
		`INSERT INTO projects (owner_id, name, slug, description, methodology, visibility, default_view, color, icon)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING id, slug`,
		claims.UserID, req.Name, slug, req.Description, methodology,
		visibility, defaultView, req.Color, req.Icon,
	).Scan(&projectID, &resultSlug)
	if err != nil {
		return writeError(c, fiber.StatusConflict, "project name already exists")
	}

	// Creator becomes owner member
	_, err = tx.Exec(ctx,
		`INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, 'owner')`,
		projectID, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to add owner")
	}

	// Create default columns based on methodology
	type col struct {
		name   string
		isDone bool
	}
	var cols []col
	switch methodology {
	case "scrum":
		cols = []col{{"To Do", false}, {"In Progress", false}, {"Review", false}, {"Done", true}}
	case "scrumban":
		cols = []col{{"Backlog", false}, {"To Do", false}, {"In Progress", false}, {"Review", false}, {"Done", true}}
	case "xp":
		cols = []col{{"Planning", false}, {"Coding", false}, {"Testing", false}, {"Done", true}}
	case "waterfall":
		cols = []col{{"Requirements", false}, {"Design", false}, {"Implementation", false}, {"Testing", false}, {"Deployment", true}}
	case "custom":
		cols = []col{{"To Do", false}, {"Done", true}}
	default: // kanban
		cols = []col{{"Backlog", false}, {"To Do", false}, {"In Progress", false}, {"Review", false}, {"Done", true}}
	}

	for i, col := range cols {
		_, err = tx.Exec(ctx,
			`INSERT INTO project_columns (project_id, name, position, is_done)
			 VALUES ($1, $2, $3, $4)`,
			projectID, col.name, i, col.isDone)
		if err != nil {
			return writeError(c, fiber.StatusInternalServerError, "failed to create default columns")
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to commit")
	}

	return writeJSON(c, fiber.StatusCreated, map[string]interface{}{"id": projectID, "slug": resultSlug})
}

func (h *Handler) GetProject(c *fiber.Ctx) error {
	slug := c.Params("projectSlug")
	claims := getClaims(c)

	var p domain.Project
	err := h.db.QueryRow(context.Background(),
		`SELECT p.id, p.owner_id, u.username, p.org_id,
		        (SELECT o.name FROM organizations o WHERE o.id = p.org_id),
		        p.name, p.slug, p.description, p.methodology, p.visibility,
		        p.default_view, p.color, p.icon,
		        (SELECT COUNT(*) FROM project_members pm2 WHERE pm2.project_id = p.id),
		        (SELECT COUNT(*) FROM project_items pi WHERE pi.project_id = p.id),
		        p.created_at, p.updated_at
		 FROM projects p
		 JOIN project_members pm ON pm.project_id = p.id
		 JOIN users u ON u.id = p.owner_id
		 WHERE p.slug = $1 AND pm.user_id = $2`, slug, claims.UserID).
		Scan(
			&p.ID, &p.OwnerID, &p.OwnerName, &p.OrgID, &p.OrgName,
			&p.Name, &p.Slug, &p.Description, &p.Methodology, &p.Visibility,
			&p.DefaultView, &p.Color, &p.Icon,
			&p.MemberCount, &p.ItemCount,
			&p.CreatedAt, &p.UpdatedAt,
		)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "project not found")
	}
	return writeJSON(c, fiber.StatusOK, p)
}

type updateProjectRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Methodology *string `json:"methodology"`
	Visibility  *string `json:"visibility"`
	DefaultView *string `json:"default_view"`
	Color       *string `json:"color"`
	Icon        *string `json:"icon"`
}

func (h *Handler) UpdateProject(c *fiber.Ctx) error {
	slug := c.Params("projectSlug")
	claims := getClaims(c)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "project not found")
	}

	var req updateProjectRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}

	tag, err := h.db.Exec(context.Background(),
		`UPDATE projects SET
			name        = COALESCE($1, name),
			description = COALESCE($2, description),
			methodology = COALESCE($3, methodology),
			visibility  = COALESCE($4, visibility),
			default_view = COALESCE($5, default_view),
			color       = COALESCE($6, color),
			icon        = COALESCE($7, icon),
			updated_at  = NOW()
		 WHERE id = $8`,
		req.Name, req.Description, req.Methodology, req.Visibility,
		req.DefaultView, req.Color, req.Icon, projectID)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusInternalServerError, "failed to update project")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) DeleteProject(c *fiber.Ctx) error {
	slug := c.Params("projectSlug")
	claims := getClaims(c)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "project not found")
	}

	role, err := h.getProjectRole(context.Background(), projectID, claims.UserID)
	if err != nil || role != "owner" {
		return writeError(c, fiber.StatusForbidden, "only the project owner can delete this project")
	}

	tag, err := h.db.Exec(context.Background(),
		`DELETE FROM projects WHERE id = $1`, projectID)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusInternalServerError, "failed to delete project")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// --- Members ---

func (h *Handler) ListProjectMembers(c *fiber.Ctx) error {
	slug := c.Params("projectSlug")
	claims := getClaims(c)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "project not found")
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT pm.id, pm.user_id, u.username, u.full_name, u.avatar_url, pm.role, pm.joined_at
		 FROM project_members pm
		 JOIN users u ON u.id = pm.user_id
		 WHERE pm.project_id = $1
		 ORDER BY pm.joined_at`, projectID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to list members")
	}
	defer rows.Close()

	var members []domain.ProjectMember
	for rows.Next() {
		var m domain.ProjectMember
		if err := rows.Scan(&m.ID, &m.UserID, &m.Username, &m.FullName, &m.AvatarURL, &m.Role, &m.JoinedAt); err != nil {
			continue
		}
		members = append(members, m)
	}
	if members == nil {
		members = []domain.ProjectMember{}
	}
	return writeJSON(c, fiber.StatusOK, members)
}

type addProjectMemberRequest struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

func (h *Handler) AddProjectMember(c *fiber.Ctx) error {
	slug := c.Params("projectSlug")
	claims := getClaims(c)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "project not found")
	}

	callerRole, err := h.getProjectRole(context.Background(), projectID, claims.UserID)
	if err != nil || (callerRole != "owner" && callerRole != "admin") {
		return writeError(c, fiber.StatusForbidden, "insufficient permissions")
	}

	var req addProjectMemberRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Username == "" {
		return writeError(c, fiber.StatusBadRequest, "username is required")
	}
	if req.Role == "" {
		req.Role = "member"
	}

	targetID, err := h.resolveUserID(context.Background(), req.Username)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "user not found")
	}

	_, err = h.db.Exec(context.Background(),
		`INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, $3)
		 ON CONFLICT (project_id, user_id) DO UPDATE SET role = $3`,
		projectID, targetID, req.Role)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to add member")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) UpdateProjectMemberRole(c *fiber.Ctx) error {
	slug := c.Params("projectSlug")
	username := c.Params("username")
	claims := getClaims(c)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "project not found")
	}

	callerRole, err := h.getProjectRole(context.Background(), projectID, claims.UserID)
	if err != nil || callerRole != "owner" {
		return writeError(c, fiber.StatusForbidden, "only the project owner can change roles")
	}

	var req struct {
		Role string `json:"role"`
	}
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Role == "" {
		return writeError(c, fiber.StatusBadRequest, "role is required")
	}

	tag, err := h.db.Exec(context.Background(),
		`UPDATE project_members SET role = $1
		 WHERE project_id = $2 AND user_id = (SELECT id FROM users WHERE username = $3)`,
		req.Role, projectID, username)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "member not found")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) RemoveProjectMember(c *fiber.Ctx) error {
	slug := c.Params("projectSlug")
	username := c.Params("username")
	claims := getClaims(c)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "project not found")
	}

	callerRole, err := h.getProjectRole(context.Background(), projectID, claims.UserID)
	if err != nil || (callerRole != "owner" && callerRole != "admin") {
		return writeError(c, fiber.StatusForbidden, "insufficient permissions")
	}

	tag, err := h.db.Exec(context.Background(),
		`DELETE FROM project_members
		 WHERE project_id = $1 AND user_id = (SELECT id FROM users WHERE username = $2)`,
		projectID, username)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "member not found")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// --- Repos ---

func (h *Handler) ListProjectRepos(c *fiber.Ctx) error {
	slug := c.Params("projectSlug")
	claims := getClaims(c)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "project not found")
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT r.id, r.owner_id, u.username, r.name, r.description, r.is_private, r.default_branch, r.org_id, r.created_at, r.updated_at
		 FROM repositories r
		 JOIN project_repos pr ON pr.repo_id = r.id
		 JOIN users u ON u.id = r.owner_id
		 WHERE pr.project_id = $1
		 ORDER BY r.updated_at DESC`, projectID)
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

func (h *Handler) LinkProjectRepo(c *fiber.Ctx) error {
	slug := c.Params("projectSlug")
	claims := getClaims(c)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "project not found")
	}

	var req struct {
		RepoID int64 `json:"repo_id"`
	}
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.RepoID == 0 {
		return writeError(c, fiber.StatusBadRequest, "repo_id is required")
	}

	_, err = h.db.Exec(context.Background(),
		`INSERT INTO project_repos (project_id, repo_id) VALUES ($1, $2)
		 ON CONFLICT (project_id, repo_id) DO NOTHING`,
		projectID, req.RepoID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to link repo")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) UnlinkProjectRepo(c *fiber.Ctx) error {
	slug := c.Params("projectSlug")
	claims := getClaims(c)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "project not found")
	}

	repoID, err := strconv.ParseInt(c.Params("repoId"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid repo id")
	}

	tag, err := h.db.Exec(context.Background(),
		`DELETE FROM project_repos WHERE project_id = $1 AND repo_id = $2`,
		projectID, repoID)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "repo link not found")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// --- Columns ---

func (h *Handler) ListProjectColumns(c *fiber.Ctx) error {
	slug := c.Params("projectSlug")
	claims := getClaims(c)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "project not found")
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT c.id, c.project_id, c.name, c.color, c.position, c.wip_limit, c.is_done,
		        (SELECT COUNT(*) FROM project_items pi WHERE pi.column_id = c.id),
		        c.created_at
		 FROM project_columns c
		 WHERE c.project_id = $1
		 ORDER BY c.position`, projectID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to list columns")
	}
	defer rows.Close()

	var columns []domain.ProjectColumn
	for rows.Next() {
		var col domain.ProjectColumn
		if err := rows.Scan(&col.ID, &col.ProjectID, &col.Name, &col.Color, &col.Position,
			&col.WIPLimit, &col.IsDone, &col.ItemCount, &col.CreatedAt); err != nil {
			continue
		}
		columns = append(columns, col)
	}
	if columns == nil {
		columns = []domain.ProjectColumn{}
	}
	return writeJSON(c, fiber.StatusOK, columns)
}

type createColumnRequest struct {
	Name     string `json:"name"`
	Color    string `json:"color"`
	WIPLimit int    `json:"wip_limit"`
	IsDone   bool   `json:"is_done"`
}

func (h *Handler) CreateProjectColumn(c *fiber.Ctx) error {
	slug := c.Params("projectSlug")
	claims := getClaims(c)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "project not found")
	}

	var req createColumnRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Name == "" {
		return writeError(c, fiber.StatusBadRequest, "name is required")
	}

	var maxPos int
	_ = h.db.QueryRow(context.Background(),
		`SELECT COALESCE(MAX(position), -1) FROM project_columns WHERE project_id = $1`,
		projectID).Scan(&maxPos)

	var id int64
	err = h.db.QueryRow(context.Background(),
		`INSERT INTO project_columns (project_id, name, color, position, wip_limit, is_done)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		projectID, req.Name, req.Color, maxPos+1, req.WIPLimit, req.IsDone,
	).Scan(&id)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to create column")
	}
	return writeJSON(c, fiber.StatusCreated, map[string]interface{}{"id": id})
}

type updateColumnRequest struct {
	Name     *string `json:"name"`
	Color    *string `json:"color"`
	WIPLimit *int    `json:"wip_limit"`
	IsDone   *bool   `json:"is_done"`
}

func (h *Handler) UpdateProjectColumn(c *fiber.Ctx) error {
	slug := c.Params("projectSlug")
	claims := getClaims(c)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "project not found")
	}

	columnID, err := strconv.ParseInt(c.Params("columnId"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid column id")
	}

	var req updateColumnRequest
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}

	tag, err := h.db.Exec(context.Background(),
		`UPDATE project_columns SET
			name      = COALESCE($1, name),
			color     = COALESCE($2, color),
			wip_limit = COALESCE($3, wip_limit),
			is_done   = COALESCE($4, is_done)
		 WHERE id = $5 AND project_id = $6`,
		req.Name, req.Color, req.WIPLimit, req.IsDone, columnID, projectID)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "column not found")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) DeleteProjectColumn(c *fiber.Ctx) error {
	slug := c.Params("projectSlug")
	claims := getClaims(c)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "project not found")
	}

	columnID, err := strconv.ParseInt(c.Params("columnId"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid column id")
	}

	// Refuse to delete a column that still contains items
	var itemCount int
	_ = h.db.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM project_items WHERE column_id = $1`, columnID).Scan(&itemCount)
	if itemCount > 0 {
		return writeError(c, fiber.StatusConflict, "column still has items; move them before deleting")
	}

	tag, err := h.db.Exec(context.Background(),
		`DELETE FROM project_columns WHERE id = $1 AND project_id = $2`, columnID, projectID)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "column not found")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) ReorderProjectColumns(c *fiber.Ctx) error {
	slug := c.Params("projectSlug")
	claims := getClaims(c)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "project not found")
	}

	var req struct {
		ColumnIDs []int64 `json:"column_ids"`
	}
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if len(req.ColumnIDs) == 0 {
		return writeError(c, fiber.StatusBadRequest, "column_ids is required")
	}

	ctx := context.Background()
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to begin transaction")
	}
	defer tx.Rollback(ctx)

	for i, colID := range req.ColumnIDs {
		_, err = tx.Exec(ctx,
			`UPDATE project_columns SET position = $1 WHERE id = $2 AND project_id = $3`,
			i, colID, projectID)
		if err != nil {
			return writeError(c, fiber.StatusInternalServerError, "failed to reorder columns")
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to commit")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// --- Swimlanes ---

func (h *Handler) ListProjectSwimlanes(c *fiber.Ctx) error {
	slug := c.Params("projectSlug")
	claims := getClaims(c)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "project not found")
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT id, project_id, name, position, created_at
		 FROM project_swimlanes
		 WHERE project_id = $1
		 ORDER BY position`, projectID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to list swimlanes")
	}
	defer rows.Close()

	var swimlanes []domain.ProjectSwimlane
	for rows.Next() {
		var s domain.ProjectSwimlane
		if err := rows.Scan(&s.ID, &s.ProjectID, &s.Name, &s.Position, &s.CreatedAt); err != nil {
			continue
		}
		swimlanes = append(swimlanes, s)
	}
	if swimlanes == nil {
		swimlanes = []domain.ProjectSwimlane{}
	}
	return writeJSON(c, fiber.StatusOK, swimlanes)
}

func (h *Handler) CreateProjectSwimlane(c *fiber.Ctx) error {
	slug := c.Params("projectSlug")
	claims := getClaims(c)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "project not found")
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Name == "" {
		return writeError(c, fiber.StatusBadRequest, "name is required")
	}

	var maxPos int
	_ = h.db.QueryRow(context.Background(),
		`SELECT COALESCE(MAX(position), -1) FROM project_swimlanes WHERE project_id = $1`,
		projectID).Scan(&maxPos)

	var id int64
	err = h.db.QueryRow(context.Background(),
		`INSERT INTO project_swimlanes (project_id, name, position)
		 VALUES ($1, $2, $3) RETURNING id`,
		projectID, req.Name, maxPos+1,
	).Scan(&id)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to create swimlane")
	}
	return writeJSON(c, fiber.StatusCreated, map[string]interface{}{"id": id})
}

func (h *Handler) UpdateProjectSwimlane(c *fiber.Ctx) error {
	slug := c.Params("projectSlug")
	claims := getClaims(c)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "project not found")
	}

	swimlaneID, err := strconv.ParseInt(c.Params("swimlaneId"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid swimlane id")
	}

	var req struct {
		Name     *string `json:"name"`
		Position *int    `json:"position"`
	}
	if err := readJSON(c, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}

	tag, err := h.db.Exec(context.Background(),
		`UPDATE project_swimlanes SET
			name     = COALESCE($1, name),
			position = COALESCE($2, position)
		 WHERE id = $3 AND project_id = $4`,
		req.Name, req.Position, swimlaneID, projectID)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "swimlane not found")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) DeleteProjectSwimlane(c *fiber.Ctx) error {
	slug := c.Params("projectSlug")
	claims := getClaims(c)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "project not found")
	}

	swimlaneID, err := strconv.ParseInt(c.Params("swimlaneId"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid swimlane id")
	}

	tag, err := h.db.Exec(context.Background(),
		`DELETE FROM project_swimlanes WHERE id = $1 AND project_id = $2`,
		swimlaneID, projectID)
	if err != nil || tag.RowsAffected() == 0 {
		return writeError(c, fiber.StatusNotFound, "swimlane not found")
	}
	return c.SendStatus(fiber.StatusNoContent)
}
