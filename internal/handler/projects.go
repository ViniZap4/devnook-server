package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/ViniZap4/devnook-server/internal/domain"
	"github.com/go-chi/chi/v5"
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

func (h *Handler) ListProjects(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
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
		writeError(w, http.StatusInternalServerError, "failed to list projects")
		return
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
	writeJSON(w, http.StatusOK, projects)
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

func (h *Handler) CreateProject(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	var req createProjectRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
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
		writeError(w, http.StatusInternalServerError, "failed to begin transaction")
		return
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
		writeError(w, http.StatusConflict, "project name already exists")
		return
	}

	// Creator becomes owner member
	_, err = tx.Exec(ctx,
		`INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, 'owner')`,
		projectID, claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add owner")
		return
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

	for i, c := range cols {
		_, err = tx.Exec(ctx,
			`INSERT INTO project_columns (project_id, name, position, is_done)
			 VALUES ($1, $2, $3, $4)`,
			projectID, c.name, i, c.isDone)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create default columns")
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": projectID, "slug": resultSlug})
}

func (h *Handler) GetProject(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "projectSlug")
	claims := getClaims(r)

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
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	writeJSON(w, http.StatusOK, p)
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

func (h *Handler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "projectSlug")
	claims := getClaims(r)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	var req updateProjectRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
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
		writeError(w, http.StatusInternalServerError, "failed to update project")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "projectSlug")
	claims := getClaims(r)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	role, err := h.getProjectRole(context.Background(), projectID, claims.UserID)
	if err != nil || role != "owner" {
		writeError(w, http.StatusForbidden, "only the project owner can delete this project")
		return
	}

	tag, err := h.db.Exec(context.Background(),
		`DELETE FROM projects WHERE id = $1`, projectID)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusInternalServerError, "failed to delete project")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Members ---

func (h *Handler) ListProjectMembers(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "projectSlug")
	claims := getClaims(r)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT pm.id, pm.user_id, u.username, u.full_name, u.avatar_url, pm.role, pm.joined_at
		 FROM project_members pm
		 JOIN users u ON u.id = pm.user_id
		 WHERE pm.project_id = $1
		 ORDER BY pm.joined_at`, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list members")
		return
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
	writeJSON(w, http.StatusOK, members)
}

type addProjectMemberRequest struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

func (h *Handler) AddProjectMember(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "projectSlug")
	claims := getClaims(r)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	callerRole, err := h.getProjectRole(context.Background(), projectID, claims.UserID)
	if err != nil || (callerRole != "owner" && callerRole != "admin") {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var req addProjectMemberRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Username == "" {
		writeError(w, http.StatusBadRequest, "username is required")
		return
	}
	if req.Role == "" {
		req.Role = "member"
	}

	targetID, err := h.resolveUserID(context.Background(), req.Username)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	_, err = h.db.Exec(context.Background(),
		`INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, $3)
		 ON CONFLICT (project_id, user_id) DO UPDATE SET role = $3`,
		projectID, targetID, req.Role)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add member")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) UpdateProjectMemberRole(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "projectSlug")
	username := chi.URLParam(r, "username")
	claims := getClaims(r)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	callerRole, err := h.getProjectRole(context.Background(), projectID, claims.UserID)
	if err != nil || callerRole != "owner" {
		writeError(w, http.StatusForbidden, "only the project owner can change roles")
		return
	}

	var req struct {
		Role string `json:"role"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Role == "" {
		writeError(w, http.StatusBadRequest, "role is required")
		return
	}

	tag, err := h.db.Exec(context.Background(),
		`UPDATE project_members SET role = $1
		 WHERE project_id = $2 AND user_id = (SELECT id FROM users WHERE username = $3)`,
		req.Role, projectID, username)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RemoveProjectMember(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "projectSlug")
	username := chi.URLParam(r, "username")
	claims := getClaims(r)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	callerRole, err := h.getProjectRole(context.Background(), projectID, claims.UserID)
	if err != nil || (callerRole != "owner" && callerRole != "admin") {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	tag, err := h.db.Exec(context.Background(),
		`DELETE FROM project_members
		 WHERE project_id = $1 AND user_id = (SELECT id FROM users WHERE username = $2)`,
		projectID, username)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Repos ---

func (h *Handler) ListProjectRepos(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "projectSlug")
	claims := getClaims(r)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT r.id, r.owner_id, u.username, r.name, r.description, r.is_private, r.default_branch, r.org_id, r.created_at, r.updated_at
		 FROM repositories r
		 JOIN project_repos pr ON pr.repo_id = r.id
		 JOIN users u ON u.id = r.owner_id
		 WHERE pr.project_id = $1
		 ORDER BY r.updated_at DESC`, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list repos")
		return
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
	writeJSON(w, http.StatusOK, repos)
}

func (h *Handler) LinkProjectRepo(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "projectSlug")
	claims := getClaims(r)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	var req struct {
		RepoID int64 `json:"repo_id"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.RepoID == 0 {
		writeError(w, http.StatusBadRequest, "repo_id is required")
		return
	}

	_, err = h.db.Exec(context.Background(),
		`INSERT INTO project_repos (project_id, repo_id) VALUES ($1, $2)
		 ON CONFLICT (project_id, repo_id) DO NOTHING`,
		projectID, req.RepoID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to link repo")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) UnlinkProjectRepo(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "projectSlug")
	claims := getClaims(r)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	repoID, err := strconv.ParseInt(chi.URLParam(r, "repoId"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repo id")
		return
	}

	tag, err := h.db.Exec(context.Background(),
		`DELETE FROM project_repos WHERE project_id = $1 AND repo_id = $2`,
		projectID, repoID)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "repo link not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Columns ---

func (h *Handler) ListProjectColumns(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "projectSlug")
	claims := getClaims(r)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT c.id, c.project_id, c.name, c.color, c.position, c.wip_limit, c.is_done,
		        (SELECT COUNT(*) FROM project_items pi WHERE pi.column_id = c.id),
		        c.created_at
		 FROM project_columns c
		 WHERE c.project_id = $1
		 ORDER BY c.position`, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list columns")
		return
	}
	defer rows.Close()

	var columns []domain.ProjectColumn
	for rows.Next() {
		var c domain.ProjectColumn
		if err := rows.Scan(&c.ID, &c.ProjectID, &c.Name, &c.Color, &c.Position,
			&c.WIPLimit, &c.IsDone, &c.ItemCount, &c.CreatedAt); err != nil {
			continue
		}
		columns = append(columns, c)
	}
	if columns == nil {
		columns = []domain.ProjectColumn{}
	}
	writeJSON(w, http.StatusOK, columns)
}

type createColumnRequest struct {
	Name     string `json:"name"`
	Color    string `json:"color"`
	WIPLimit int    `json:"wip_limit"`
	IsDone   bool   `json:"is_done"`
}

func (h *Handler) CreateProjectColumn(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "projectSlug")
	claims := getClaims(r)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	var req createColumnRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
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
		writeError(w, http.StatusInternalServerError, "failed to create column")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id})
}

type updateColumnRequest struct {
	Name     *string `json:"name"`
	Color    *string `json:"color"`
	WIPLimit *int    `json:"wip_limit"`
	IsDone   *bool   `json:"is_done"`
}

func (h *Handler) UpdateProjectColumn(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "projectSlug")
	claims := getClaims(r)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	columnID, err := strconv.ParseInt(chi.URLParam(r, "columnId"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid column id")
		return
	}

	var req updateColumnRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
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
		writeError(w, http.StatusNotFound, "column not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) DeleteProjectColumn(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "projectSlug")
	claims := getClaims(r)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	columnID, err := strconv.ParseInt(chi.URLParam(r, "columnId"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid column id")
		return
	}

	// Refuse to delete a column that still contains items
	var itemCount int
	_ = h.db.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM project_items WHERE column_id = $1`, columnID).Scan(&itemCount)
	if itemCount > 0 {
		writeError(w, http.StatusConflict, "column still has items; move them before deleting")
		return
	}

	tag, err := h.db.Exec(context.Background(),
		`DELETE FROM project_columns WHERE id = $1 AND project_id = $2`, columnID, projectID)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "column not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ReorderProjectColumns(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "projectSlug")
	claims := getClaims(r)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	var req struct {
		ColumnIDs []int64 `json:"column_ids"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.ColumnIDs) == 0 {
		writeError(w, http.StatusBadRequest, "column_ids is required")
		return
	}

	ctx := context.Background()
	tx, err := h.db.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin transaction")
		return
	}
	defer tx.Rollback(ctx)

	for i, colID := range req.ColumnIDs {
		_, err = tx.Exec(ctx,
			`UPDATE project_columns SET position = $1 WHERE id = $2 AND project_id = $3`,
			i, colID, projectID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to reorder columns")
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Swimlanes ---

func (h *Handler) ListProjectSwimlanes(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "projectSlug")
	claims := getClaims(r)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	rows, err := h.db.Query(context.Background(),
		`SELECT id, project_id, name, position, created_at
		 FROM project_swimlanes
		 WHERE project_id = $1
		 ORDER BY position`, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list swimlanes")
		return
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
	writeJSON(w, http.StatusOK, swimlanes)
}

func (h *Handler) CreateProjectSwimlane(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "projectSlug")
	claims := getClaims(r)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
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
		writeError(w, http.StatusInternalServerError, "failed to create swimlane")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id})
}

func (h *Handler) UpdateProjectSwimlane(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "projectSlug")
	claims := getClaims(r)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	swimlaneID, err := strconv.ParseInt(chi.URLParam(r, "swimlaneId"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid swimlane id")
		return
	}

	var req struct {
		Name     *string `json:"name"`
		Position *int    `json:"position"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	tag, err := h.db.Exec(context.Background(),
		`UPDATE project_swimlanes SET
			name     = COALESCE($1, name),
			position = COALESCE($2, position)
		 WHERE id = $3 AND project_id = $4`,
		req.Name, req.Position, swimlaneID, projectID)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "swimlane not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) DeleteProjectSwimlane(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "projectSlug")
	claims := getClaims(r)

	projectID, err := h.getProjectBySlug(context.Background(), slug, claims.UserID)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	swimlaneID, err := strconv.ParseInt(chi.URLParam(r, "swimlaneId"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid swimlane id")
		return
	}

	tag, err := h.db.Exec(context.Background(),
		`DELETE FROM project_swimlanes WHERE id = $1 AND project_id = $2`,
		swimlaneID, projectID)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "swimlane not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
