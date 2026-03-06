package main

import (
	"context"
	"log"
	"net/http"
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
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
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

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)

	allowedOrigins := []string{"*"}
	if cfg.AllowedOrigins != "" {
		allowedOrigins = strings.Split(cfg.AllowedOrigins, ",")
		for i, o := range allowedOrigins {
			allowedOrigins[i] = strings.TrimSpace(o)
		}
	}
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	h := handler.New(db, cfg, hub)

	r.Get("/api/v1/health", h.Health)

	r.Route("/api/v1/auth", func(r chi.Router) {
		r.Get("/setup", h.NeedsSetup)
		r.Post("/setup", h.Setup)
		r.Post("/register", h.Register)
		r.Post("/login", h.Login)
	})

	// Public API (static routes BEFORE wildcard to avoid conflicts)
	r.Get("/api/v1/explore/repos", h.ExploreRepos)
	r.Get("/api/v1/users/search", h.SearchUsers)
	r.Get("/api/v1/users/{username}", h.GetUserProfile)
	r.Get("/api/v1/users/{username}/starred", h.ListUserStarred)

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(h.AuthMiddleware)

		// User
		r.Get("/users/me", h.GetCurrentUser)
		r.Put("/users/me", h.UpdateProfile)
		r.Put("/users/me/password", h.ChangePassword)
		r.Get("/dashboard/stats", h.GetDashboardStats)
		r.Get("/dashboard/activity", h.GetDashboardActivity)

		// User social (me routes first)
		r.Get("/users/me/blocked", h.ListBlockedUsers)
		r.Put("/users/me/status", h.SetStatus)
		r.Delete("/users/me/status", h.ClearStatus)

		// User social (parameterized)
		r.Post("/users/{username}/follow", h.FollowUser)
		r.Delete("/users/{username}/follow", h.UnfollowUser)
		r.Get("/users/{username}/follow", h.IsFollowing)
		r.Get("/users/{username}/followers", h.GetFollowers)
		r.Get("/users/{username}/following", h.GetFollowing)
		r.Post("/users/{username}/block", h.BlockUser)
		r.Delete("/users/{username}/block", h.UnblockUser)
		r.Get("/users/{username}/block", h.IsBlocked)
		r.Get("/users/{username}/status", h.GetStatus)

		// User preferences
		r.Get("/users/me/preferences", h.GetPreferences)
		r.Put("/users/me/preferences", h.UpdatePreferences)

		// SSH keys
		r.Get("/users/me/keys", h.ListSSHKeys)
		r.Post("/users/me/keys", h.CreateSSHKey)
		r.Delete("/users/me/keys/{id}", h.DeleteSSHKey)

		// Notifications
		r.Get("/notifications", h.ListNotifications)
		r.Get("/notifications/unread", h.UnreadNotificationCount)
		r.Put("/notifications/{id}/read", h.MarkNotificationRead)
		r.Put("/notifications/read-all", h.MarkAllNotificationsRead)

		// Repositories
		r.Get("/repos", h.ListRepos)
		r.Post("/repos", h.CreateRepo)
		r.Get("/repos/{owner}/{name}", h.GetRepo)
		r.Put("/repos/{owner}/{name}", h.UpdateRepo)
		r.Delete("/repos/{owner}/{name}", h.DeleteRepo)

		// Stars
		r.Put("/repos/{owner}/{name}/star", h.StarRepo)
		r.Delete("/repos/{owner}/{name}/star", h.UnstarRepo)
		r.Get("/repos/{owner}/{name}/star", h.IsStarred)
		r.Get("/repos/{owner}/{name}/stargazers", h.ListStargazers)

		// Forks
		r.Post("/repos/{owner}/{name}/forks", h.ForkRepo)
		r.Get("/repos/{owner}/{name}/forks", h.ListForks)

		// Git browsing
		r.Get("/repos/{owner}/{name}/tree/{ref}/*", h.GetTree)
		r.Get("/repos/{owner}/{name}/blob/{ref}/*", h.GetBlob)
		r.Get("/repos/{owner}/{name}/commits", h.GetCommits)
		r.Get("/repos/{owner}/{name}/commits/{hash}", h.GetCommitDetail)
		r.Get("/repos/{owner}/{name}/branches", h.GetBranches)
		r.Get("/repos/{owner}/{name}/tags", h.GetTags)
		r.Get("/repos/{owner}/{name}/readme", h.GetReadme)
		r.Get("/repos/{owner}/{name}/blame/{ref}/*", h.GetBlame)
		r.Get("/repos/{owner}/{name}/compare", h.CompareBranches)

		// File editor
		r.Post("/repos/{owner}/{name}/contents/*", h.CreateFile)
		r.Put("/repos/{owner}/{name}/contents/*", h.UpdateFile)
		r.Delete("/repos/{owner}/{name}/contents/*", h.DeleteFile)

		// Branch management
		r.Post("/repos/{owner}/{name}/branches", h.CreateBranch)
		r.Delete("/repos/{owner}/{name}/branches/{branch}", h.DeleteBranch)

		// Archive
		r.Get("/repos/{owner}/{name}/archive/{ref}", h.GetArchive)

		// Language stats
		r.Get("/repos/{owner}/{name}/languages", h.GetLanguages)

		// Contributors
		r.Get("/repos/{owner}/{name}/contributors", h.GetContributors)

		// Labels
		r.Get("/repos/{owner}/{name}/labels", h.ListLabels)
		r.Post("/repos/{owner}/{name}/labels", h.CreateLabel)
		r.Put("/repos/{owner}/{name}/labels/{id}", h.UpdateLabel)
		r.Delete("/repos/{owner}/{name}/labels/{id}", h.DeleteLabel)

		// Milestones
		r.Get("/repos/{owner}/{name}/milestones", h.ListMilestones)
		r.Post("/repos/{owner}/{name}/milestones", h.CreateMilestone)
		r.Put("/repos/{owner}/{name}/milestones/{id}", h.UpdateMilestone)
		r.Delete("/repos/{owner}/{name}/milestones/{id}", h.DeleteMilestone)

		// Releases
		r.Get("/repos/{owner}/{name}/releases", h.ListReleases)
		r.Post("/repos/{owner}/{name}/releases", h.CreateRelease)
		r.Get("/repos/{owner}/{name}/releases/{id}", h.GetRelease)
		r.Put("/repos/{owner}/{name}/releases/{id}", h.UpdateRelease)
		r.Delete("/repos/{owner}/{name}/releases/{id}", h.DeleteRelease)

		// Collaborators
		r.Get("/repos/{owner}/{name}/collaborators", h.ListCollaborators)
		r.Post("/repos/{owner}/{name}/collaborators", h.AddCollaborator)
		r.Delete("/repos/{owner}/{name}/collaborators/{username}", h.RemoveCollaborator)
		r.Post("/repos/{owner}/{name}/transfer", h.TransferRepo)

		// Webhooks
		r.Get("/repos/{owner}/{name}/hooks", h.ListWebhooks)
		r.Post("/repos/{owner}/{name}/hooks", h.CreateWebhook)
		r.Put("/repos/{owner}/{name}/hooks/{id}", h.UpdateWebhook)
		r.Delete("/repos/{owner}/{name}/hooks/{id}", h.DeleteWebhook)

		// Issues
		r.Get("/repos/{owner}/{name}/issues", h.ListIssues)
		r.Post("/repos/{owner}/{name}/issues", h.CreateIssue)
		r.Get("/repos/{owner}/{name}/issues/{number}", h.GetIssue)
		r.Put("/repos/{owner}/{name}/issues/{number}", h.UpdateIssue)
		r.Get("/repos/{owner}/{name}/issues/{number}/comments", h.ListIssueComments)
		r.Post("/repos/{owner}/{name}/issues/{number}/comments", h.CreateIssueComment)
		r.Put("/repos/{owner}/{name}/issues/{number}/comments/{id}", h.UpdateIssueComment)
		r.Delete("/repos/{owner}/{name}/issues/{number}/comments/{id}", h.DeleteIssueComment)
		r.Post("/repos/{owner}/{name}/issues/{number}/labels", h.AddIssueLabel)
		r.Delete("/repos/{owner}/{name}/issues/{number}/labels/{labelId}", h.RemoveIssueLabel)

		// Pull Requests
		r.Get("/repos/{owner}/{name}/pulls", h.ListPullRequests)
		r.Post("/repos/{owner}/{name}/pulls", h.CreatePullRequest)
		r.Get("/repos/{owner}/{name}/pulls/{number}", h.GetPullRequest)
		r.Put("/repos/{owner}/{name}/pulls/{number}", h.UpdatePullRequest)
		r.Post("/repos/{owner}/{name}/pulls/{number}/merge", h.MergePullRequest)
		r.Get("/repos/{owner}/{name}/pulls/{number}/comments", h.ListPRComments)
		r.Post("/repos/{owner}/{name}/pulls/{number}/comments", h.CreatePRComment)
		r.Get("/repos/{owner}/{name}/pulls/{number}/reviews", h.ListPRReviews)
		r.Post("/repos/{owner}/{name}/pulls/{number}/reviews", h.CreatePRReview)

		// Organizations
		r.Get("/orgs", h.ListOrgs)
		r.Post("/orgs", h.CreateOrg)
		r.Get("/orgs/{name}", h.GetOrg)
		r.Put("/orgs/{name}", h.UpdateOrg)
		r.Delete("/orgs/{name}", h.DeleteOrg)
		r.Get("/orgs/{name}/members", h.ListOrgMembers)
		r.Post("/orgs/{name}/members", h.AddOrgMember)
		r.Put("/orgs/{name}/members/{username}", h.UpdateOrgMember)
		r.Delete("/orgs/{name}/members/{username}", h.RemoveOrgMember)
		r.Get("/orgs/{name}/repos", h.ListOrgRepos)
		r.Post("/orgs/{name}/repos", h.CreateOrgRepo)

		// Posts / Feed
		r.Get("/posts", h.GetFeed)
		r.Post("/posts", h.CreatePost)
		r.Get("/posts/{id}", h.GetPost)
		r.Put("/posts/{id}", h.UpdatePost)
		r.Delete("/posts/{id}", h.DeletePost)
		r.Post("/posts/{id}/like", h.LikePost)
		r.Delete("/posts/{id}/like", h.UnlikePost)
		r.Post("/posts/{id}/repost", h.RepostPost)
		r.Get("/posts/{id}/comments", h.GetPostComments)
		r.Post("/posts/{id}/comments", h.AddPostComment)
		r.Delete("/posts/{id}/comments/{commentId}", h.RemovePostComment)
		r.Get("/users/{username}/posts", h.GetUserPosts)

		// Messages / Chat
		r.Get("/messages/conversations", h.ListConversations)
		r.Post("/messages/conversations", h.CreateConversation)
		r.Get("/messages/conversations/{id}", h.GetConversation)
		r.Get("/messages/conversations/{conversationId}/messages", h.ListMessages)
		r.Post("/messages/conversations/{conversationId}/messages", h.SendMessage)
		r.Put("/messages/conversations/{conversationId}/messages/{messageId}", h.EditMessage)
		r.Delete("/messages/conversations/{conversationId}/messages/{messageId}", h.DeleteMessage)
		r.Post("/messages/conversations/{conversationId}/messages/{messageId}/react", h.ReactToMessage)
		r.Get("/messages/unread", h.UnreadMessageCount)

		// Shortcuts
		r.Get("/shortcuts", h.ListShortcuts)
		r.Post("/shortcuts", h.CreateShortcut)
		r.Put("/shortcuts/{id}", h.UpdateShortcut)
		r.Delete("/shortcuts/{id}", h.DeleteShortcut)

		// Admin
		r.Route("/admin", func(r chi.Router) {
			r.Get("/stats", h.AdminStats)
			r.Get("/users", h.AdminListUsers)
			r.Get("/users/{username}", h.AdminGetUser)
			r.Put("/users/{username}", h.AdminUpdateUser)
			r.Delete("/users/{username}", h.AdminDeleteUser)
			r.Get("/repos", h.AdminListRepos)
			r.Get("/orgs", h.AdminListOrgs)
		})
	})

	// Git Smart HTTP protocol
	r.Get("/{owner}/{repo}.git/info/refs", h.GitInfoRefs)
	r.Post("/{owner}/{repo}.git/git-upload-pack", h.GitUploadPack)
	r.Post("/{owner}/{repo}.git/git-receive-pack", h.GitReceivePack)

	// WebSocket (auth via ?token= query param)
	r.Get("/ws", func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token == "" {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}
		claims, err := auth.ValidateToken(token, cfg.Secret)
		if err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		hub.HandleWebSocket(w, r, claims.UserID)
	})

	port := cfg.Port
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		<-ctx.Done()
		log.Println("shutting down server...")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("server shutdown error: %v", err)
		}
	}()

	log.Printf("devnook-server listening on :%s", port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
