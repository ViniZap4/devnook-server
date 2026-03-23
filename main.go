package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ViniZap4/devnook-server/internal/auth"
	"github.com/ViniZap4/devnook-server/internal/config"
	"github.com/ViniZap4/devnook-server/internal/database"
	"github.com/ViniZap4/devnook-server/internal/handler"
	"github.com/ViniZap4/devnook-server/internal/ws"
	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

func main() {
	cfg := config.Load()

	db, err := database.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database connection failed: %v", err)
	}
	defer db.Close()

	if err := database.Migrate(db); err != nil {
		log.Fatalf("database migration failed: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	hub := ws.NewHub()
	go hub.Run(ctx)

	app := fiber.New(fiber.Config{
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
		// Allow streaming for git protocol
		StreamRequestBody:    true,
		DisableStartupMessage: true,
	})

	app.Use(logger.New())
	app.Use(recover.New())
	app.Use(requestid.New())

	allowedOrigins := "*"
	if cfg.AllowedOrigins != "" {
		origins := strings.Split(cfg.AllowedOrigins, ",")
		for i, o := range origins {
			origins[i] = strings.TrimSpace(o)
		}
		allowedOrigins = strings.Join(origins, ",")
	}
	app.Use(cors.New(cors.Config{
		AllowOrigins:     allowedOrigins,
		AllowMethods:     "GET,POST,PUT,DELETE,OPTIONS",
		AllowHeaders:     "Accept,Authorization,Content-Type",
		AllowCredentials: true,
		MaxAge:           300,
	}))

	h := handler.New(db, cfg, hub)

	app.Get("/api/v1/health", h.Health)

	// Auth (public)
	authGroup := app.Group("/api/v1/auth")
	authGroup.Get("/setup", h.NeedsSetup)
	authGroup.Post("/setup", h.Setup)
	authGroup.Post("/register", h.Register)
	authGroup.Post("/login", h.Login)

	// Public API (static routes BEFORE wildcard to avoid conflicts)
	app.Get("/api/v1/explore/repos", h.ExploreRepos)
	app.Get("/api/v1/users/search", h.SearchUsers)
	app.Get("/api/v1/users/:username", h.GetUserProfile)
	app.Get("/api/v1/users/:username/starred", h.ListUserStarred)

	// Authenticated API
	api := app.Group("/api/v1", h.AuthMiddleware)

	// User
	api.Get("/users/me", h.GetCurrentUser)
	api.Put("/users/me", h.UpdateProfile)
	api.Put("/users/me/password", h.ChangePassword)
	api.Get("/dashboard/stats", h.GetDashboardStats)
	api.Get("/dashboard/activity", h.GetDashboardActivity)

	// User social (me routes first)
	api.Get("/users/me/blocked", h.ListBlockedUsers)
	api.Put("/users/me/status", h.SetStatus)
	api.Delete("/users/me/status", h.ClearStatus)

	// User social (parameterized)
	api.Post("/users/:username/follow", h.FollowUser)
	api.Delete("/users/:username/follow", h.UnfollowUser)
	api.Get("/users/:username/follow", h.IsFollowing)
	api.Get("/users/:username/followers", h.GetFollowers)
	api.Get("/users/:username/following", h.GetFollowing)
	api.Post("/users/:username/block", h.BlockUser)
	api.Delete("/users/:username/block", h.UnblockUser)
	api.Get("/users/:username/block", h.IsBlocked)
	api.Get("/users/:username/status", h.GetStatus)

	// User preferences
	api.Get("/users/me/preferences", h.GetPreferences)
	api.Put("/users/me/preferences", h.UpdatePreferences)

	// SSH keys
	api.Get("/users/me/keys", h.ListSSHKeys)
	api.Post("/users/me/keys", h.CreateSSHKey)
	api.Delete("/users/me/keys/:id", h.DeleteSSHKey)

	// Notifications
	api.Get("/notifications", h.ListNotifications)
	api.Get("/notifications/unread", h.UnreadNotificationCount)
	api.Put("/notifications/:id/read", h.MarkNotificationRead)
	api.Put("/notifications/read-all", h.MarkAllNotificationsRead)

	// Repositories
	api.Get("/repos", h.ListRepos)
	api.Post("/repos", h.CreateRepo)
	api.Get("/repos/:owner/:name", h.GetRepo)
	api.Put("/repos/:owner/:name", h.UpdateRepo)
	api.Delete("/repos/:owner/:name", h.DeleteRepo)

	// Stars
	api.Put("/repos/:owner/:name/star", h.StarRepo)
	api.Delete("/repos/:owner/:name/star", h.UnstarRepo)
	api.Get("/repos/:owner/:name/star", h.IsStarred)
	api.Get("/repos/:owner/:name/stargazers", h.ListStargazers)

	// Forks
	api.Post("/repos/:owner/:name/forks", h.ForkRepo)
	api.Get("/repos/:owner/:name/forks", h.ListForks)

	// Git browsing
	api.Get("/repos/:owner/:name/tree/:ref/*", h.GetTree)
	api.Get("/repos/:owner/:name/blob/:ref/*", h.GetBlob)
	api.Get("/repos/:owner/:name/commits", h.GetCommits)
	api.Get("/repos/:owner/:name/commits/:hash", h.GetCommitDetail)
	api.Get("/repos/:owner/:name/branches", h.GetBranches)
	api.Get("/repos/:owner/:name/tags", h.GetTags)
	api.Get("/repos/:owner/:name/readme", h.GetReadme)
	api.Get("/repos/:owner/:name/blame/:ref/*", h.GetBlame)
	api.Get("/repos/:owner/:name/compare", h.CompareBranches)

	// File editor
	api.Post("/repos/:owner/:name/contents/*", h.CreateFile)
	api.Put("/repos/:owner/:name/contents/*", h.UpdateFile)
	api.Delete("/repos/:owner/:name/contents/*", h.DeleteFile)

	// Branch management
	api.Post("/repos/:owner/:name/branches", h.CreateBranch)
	api.Delete("/repos/:owner/:name/branches/:branch", h.DeleteBranch)

	// Archive
	api.Get("/repos/:owner/:name/archive/:ref", h.GetArchive)

	// Language stats
	api.Get("/repos/:owner/:name/languages", h.GetLanguages)

	// Contributors
	api.Get("/repos/:owner/:name/contributors", h.GetContributors)

	// Labels
	api.Get("/repos/:owner/:name/labels", h.ListLabels)
	api.Post("/repos/:owner/:name/labels", h.CreateLabel)
	api.Put("/repos/:owner/:name/labels/:id", h.UpdateLabel)
	api.Delete("/repos/:owner/:name/labels/:id", h.DeleteLabel)

	// Milestones
	api.Get("/repos/:owner/:name/milestones", h.ListMilestones)
	api.Post("/repos/:owner/:name/milestones", h.CreateMilestone)
	api.Put("/repos/:owner/:name/milestones/:id", h.UpdateMilestone)
	api.Delete("/repos/:owner/:name/milestones/:id", h.DeleteMilestone)

	// Releases
	api.Get("/repos/:owner/:name/releases", h.ListReleases)
	api.Post("/repos/:owner/:name/releases", h.CreateRelease)
	api.Get("/repos/:owner/:name/releases/:id", h.GetRelease)
	api.Put("/repos/:owner/:name/releases/:id", h.UpdateRelease)
	api.Delete("/repos/:owner/:name/releases/:id", h.DeleteRelease)

	// Collaborators
	api.Get("/repos/:owner/:name/collaborators", h.ListCollaborators)
	api.Post("/repos/:owner/:name/collaborators", h.AddCollaborator)
	api.Delete("/repos/:owner/:name/collaborators/:username", h.RemoveCollaborator)
	api.Post("/repos/:owner/:name/transfer", h.TransferRepo)

	// Webhooks
	api.Get("/repos/:owner/:name/hooks", h.ListWebhooks)
	api.Post("/repos/:owner/:name/hooks", h.CreateWebhook)
	api.Put("/repos/:owner/:name/hooks/:id", h.UpdateWebhook)
	api.Delete("/repos/:owner/:name/hooks/:id", h.DeleteWebhook)

	// Issues
	api.Get("/repos/:owner/:name/issues", h.ListIssues)
	api.Post("/repos/:owner/:name/issues", h.CreateIssue)
	api.Get("/repos/:owner/:name/issues/:number", h.GetIssue)
	api.Put("/repos/:owner/:name/issues/:number", h.UpdateIssue)
	api.Get("/repos/:owner/:name/issues/:number/comments", h.ListIssueComments)
	api.Post("/repos/:owner/:name/issues/:number/comments", h.CreateIssueComment)
	api.Put("/repos/:owner/:name/issues/:number/comments/:id", h.UpdateIssueComment)
	api.Delete("/repos/:owner/:name/issues/:number/comments/:id", h.DeleteIssueComment)
	api.Post("/repos/:owner/:name/issues/:number/labels", h.AddIssueLabel)
	api.Delete("/repos/:owner/:name/issues/:number/labels/:labelId", h.RemoveIssueLabel)
	api.Post("/repos/:owner/:name/issues/:number/add-to-project", h.AddIssueToProject)

	// Pull Requests
	api.Get("/repos/:owner/:name/pulls", h.ListPullRequests)
	api.Post("/repos/:owner/:name/pulls", h.CreatePullRequest)
	api.Get("/repos/:owner/:name/pulls/:number", h.GetPullRequest)
	api.Put("/repos/:owner/:name/pulls/:number", h.UpdatePullRequest)
	api.Post("/repos/:owner/:name/pulls/:number/merge", h.MergePullRequest)
	api.Get("/repos/:owner/:name/pulls/:number/comments", h.ListPRComments)
	api.Post("/repos/:owner/:name/pulls/:number/comments", h.CreatePRComment)
	api.Get("/repos/:owner/:name/pulls/:number/reviews", h.ListPRReviews)
	api.Post("/repos/:owner/:name/pulls/:number/reviews", h.CreatePRReview)

	// Organizations
	api.Get("/orgs", h.ListOrgs)
	api.Post("/orgs", h.CreateOrg)
	api.Get("/orgs/:name", h.GetOrg)
	api.Put("/orgs/:name", h.UpdateOrg)
	api.Delete("/orgs/:name", h.DeleteOrg)
	api.Get("/orgs/:name/members", h.ListOrgMembers)
	api.Post("/orgs/:name/members", h.AddOrgMember)
	api.Put("/orgs/:name/members/:username", h.UpdateOrgMember)
	api.Delete("/orgs/:name/members/:username", h.RemoveOrgMember)
	api.Get("/orgs/:name/repos", h.ListOrgRepos)
	api.Post("/orgs/:name/repos", h.CreateOrgRepo)

	// Posts / Feed
	api.Get("/posts", h.GetFeed)
	api.Post("/posts", h.CreatePost)
	api.Get("/posts/:id", h.GetPost)
	api.Put("/posts/:id", h.UpdatePost)
	api.Delete("/posts/:id", h.DeletePost)
	api.Post("/posts/:id/like", h.LikePost)
	api.Delete("/posts/:id/like", h.UnlikePost)
	api.Post("/posts/:id/repost", h.RepostPost)
	api.Get("/posts/:id/comments", h.GetPostComments)
	api.Post("/posts/:id/comments", h.AddPostComment)
	api.Delete("/posts/:id/comments/:commentId", h.RemovePostComment)
	api.Get("/users/:username/posts", h.GetUserPosts)

	// Messages / Chat
	api.Get("/messages/conversations", h.ListConversations)
	api.Post("/messages/conversations", h.CreateConversation)
	api.Get("/messages/conversations/:id", h.GetConversation)
	api.Get("/messages/conversations/:conversationId/messages", h.ListMessages)
	api.Post("/messages/conversations/:conversationId/messages", h.SendMessage)
	api.Put("/messages/conversations/:conversationId/messages/:messageId", h.EditMessage)
	api.Delete("/messages/conversations/:conversationId/messages/:messageId", h.DeleteMessage)
	api.Post("/messages/conversations/:conversationId/messages/:messageId/react", h.ReactToMessage)
	api.Post("/messages/conversations/:conversationId/typing", h.TypingIndicator)
	api.Get("/messages/unread", h.UnreadMessageCount)
	api.Post("/messages/conversations/:conversationId/call", h.InitiateCall)
	api.Delete("/messages/conversations/:conversationId", h.DeleteConversation)
	api.Post("/messages/conversations/:conversationId/read", h.MarkConversationRead)
	api.Get("/messages/conversations/:conversationId/search", h.SearchMessages)
	api.Post("/messages/conversations/:conversationId/participants", h.AddParticipant)
	api.Delete("/messages/conversations/:conversationId/participants/:username", h.RemoveParticipant)
	api.Get("/links/preview", h.GetLinkPreview)

	// Shortcuts
	api.Get("/shortcuts", h.ListShortcuts)
	api.Post("/shortcuts", h.CreateShortcut)
	api.Put("/shortcuts/:id", h.UpdateShortcut)
	api.Delete("/shortcuts/:id", h.DeleteShortcut)

	// Documentation
	api.Get("/docs/spaces", h.ListDocSpaces)
	api.Post("/docs/spaces", h.CreateDocSpace)
	api.Get("/docs/spaces/:spaceSlug", h.GetDocSpace)
	api.Put("/docs/spaces/:spaceSlug", h.UpdateDocSpace)
	api.Delete("/docs/spaces/:spaceSlug", h.DeleteDocSpace)
	api.Get("/docs/spaces/:spaceSlug/pages", h.ListDocPages)
	api.Post("/docs/spaces/:spaceSlug/pages", h.CreateDocPage)
	api.Get("/docs/spaces/:spaceSlug/pages/:pageSlug", h.GetDocPage)
	api.Put("/docs/spaces/:spaceSlug/pages/:pageSlug", h.UpdateDocPage)
	api.Delete("/docs/spaces/:spaceSlug/pages/:pageSlug", h.DeleteDocPage)
	api.Get("/docs/spaces/:spaceSlug/pages/:pageSlug/versions", h.ListDocPageVersions)

	// Projects
	api.Get("/projects", h.ListProjects)
	api.Post("/projects", h.CreateProject)
	api.Get("/projects/:projectSlug", h.GetProject)
	api.Put("/projects/:projectSlug", h.UpdateProject)
	api.Delete("/projects/:projectSlug", h.DeleteProject)
	api.Get("/projects/:projectSlug/members", h.ListProjectMembers)
	api.Post("/projects/:projectSlug/members", h.AddProjectMember)
	api.Put("/projects/:projectSlug/members/:username", h.UpdateProjectMemberRole)
	api.Delete("/projects/:projectSlug/members/:username", h.RemoveProjectMember)
	api.Get("/projects/:projectSlug/repos", h.ListProjectRepos)
	api.Post("/projects/:projectSlug/repos", h.LinkProjectRepo)
	api.Delete("/projects/:projectSlug/repos/:repoId", h.UnlinkProjectRepo)
	api.Get("/projects/:projectSlug/columns", h.ListProjectColumns)
	api.Post("/projects/:projectSlug/columns", h.CreateProjectColumn)
	api.Put("/projects/:projectSlug/columns/:columnId", h.UpdateProjectColumn)
	api.Delete("/projects/:projectSlug/columns/:columnId", h.DeleteProjectColumn)
	api.Put("/projects/:projectSlug/columns/reorder", h.ReorderProjectColumns)
	api.Get("/projects/:projectSlug/swimlanes", h.ListProjectSwimlanes)
	api.Post("/projects/:projectSlug/swimlanes", h.CreateProjectSwimlane)
	api.Put("/projects/:projectSlug/swimlanes/:swimlaneId", h.UpdateProjectSwimlane)
	api.Delete("/projects/:projectSlug/swimlanes/:swimlaneId", h.DeleteProjectSwimlane)

	// Project Sprints
	api.Get("/projects/:projectSlug/sprints", h.ListProjectSprints)
	api.Post("/projects/:projectSlug/sprints", h.CreateProjectSprint)
	api.Get("/projects/:projectSlug/sprints/:sprintId", h.GetProjectSprint)
	api.Put("/projects/:projectSlug/sprints/:sprintId", h.UpdateProjectSprint)
	api.Delete("/projects/:projectSlug/sprints/:sprintId", h.DeleteProjectSprint)
	api.Post("/projects/:projectSlug/sprints/:sprintId/start", h.StartProjectSprint)
	api.Post("/projects/:projectSlug/sprints/:sprintId/complete", h.CompleteProjectSprint)

	// Project Items & Board
	api.Get("/projects/:projectSlug/items", h.ListProjectItems)
	api.Post("/projects/:projectSlug/items", h.CreateProjectItem)
	api.Get("/projects/:projectSlug/items/:itemId", h.GetProjectItem)
	api.Put("/projects/:projectSlug/items/:itemId", h.UpdateProjectItem)
	api.Delete("/projects/:projectSlug/items/:itemId", h.DeleteProjectItem)
	api.Put("/projects/:projectSlug/items/:itemId/move", h.MoveProjectItem)
	api.Get("/projects/:projectSlug/items/:itemId/history", h.GetProjectItemHistory)
	api.Get("/projects/:projectSlug/board", h.GetProjectBoard)

	// Calendar
	api.Get("/calendar/events", h.ListCalendarEvents)
	api.Post("/calendar/events", h.CreateCalendarEvent)
	api.Get("/calendar/events/:eventId", h.GetCalendarEvent)
	api.Put("/calendar/events/:eventId", h.UpdateCalendarEvent)
	api.Delete("/calendar/events/:eventId", h.DeleteCalendarEvent)
	api.Post("/calendar/events/:eventId/rsvp", h.UpdateCalendarRSVP)
	api.Get("/calendar/unified", h.GetUnifiedCalendar)

	// Admin
	admin := api.Group("/admin")
	admin.Get("/stats", h.AdminStats)
	admin.Get("/analytics", h.AdminAnalytics)
	admin.Get("/users", h.AdminListUsers)
	admin.Get("/users/:username", h.AdminGetUser)
	admin.Put("/users/:username", h.AdminUpdateUser)
	admin.Delete("/users/:username", h.AdminDeleteUser)
	admin.Get("/repos", h.AdminListRepos)
	admin.Get("/orgs", h.AdminListOrgs)

	// Git Smart HTTP protocol
	app.Get("/:owner/:repo.git/info/refs", h.GitInfoRefs)
	app.Post("/:owner/:repo.git/git-upload-pack", h.GitUploadPack)
	app.Post("/:owner/:repo.git/git-receive-pack", h.GitReceivePack)

	// WebSocket (auth via ?token= query param)
	app.Use("/ws", func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			token := c.Query("token")
			if token == "" {
				return c.Status(fiber.StatusUnauthorized).SendString("missing token")
			}
			claims, err := auth.ValidateToken(token, cfg.Secret)
			if err != nil {
				return c.Status(fiber.StatusUnauthorized).SendString("invalid token")
			}
			c.Locals("ws_user_id", claims.UserID)
			c.Locals("ws_username", claims.Username)
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})
	app.Get("/ws", websocket.New(func(conn *websocket.Conn) {
		userID := conn.Locals("ws_user_id").(int64)
		username := conn.Locals("ws_username").(string)
		hub.HandleWebSocket(conn.Conn, userID, username)
	}))

	port := cfg.Port
	if port == "" {
		port = "8080"
	}

	go func() {
		<-ctx.Done()
		log.Println("shutting down server...")
		if err := app.ShutdownWithTimeout(10 * time.Second); err != nil {
			log.Printf("server shutdown error: %v", err)
		}
	}()

	log.Printf("devnook-server listening on :%s", port)
	if err := app.Listen(":" + port); err != nil {
		log.Fatal(err)
	}
}
