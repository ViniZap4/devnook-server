package main

import (
	"log"
	"net/http"

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

	hub := ws.NewHub()
	go hub.Run()

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
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

	// Public API (no auth required)
	r.Get("/api/v1/explore/repos", h.ExploreRepos)
	r.Get("/api/v1/users/{username}", h.GetUserProfile)

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(h.AuthMiddleware)
		r.Get("/users/me", h.GetCurrentUser)
		r.Put("/users/me", h.UpdateProfile)
		r.Get("/dashboard/stats", h.GetDashboardStats)

		// Repositories
		r.Get("/repos", h.ListRepos)
		r.Post("/repos", h.CreateRepo)
		r.Get("/repos/{owner}/{name}", h.GetRepo)
		r.Delete("/repos/{owner}/{name}", h.DeleteRepo)

		// Git browsing
		r.Get("/repos/{owner}/{name}/tree/{ref}/*", h.GetTree)
		r.Get("/repos/{owner}/{name}/blob/{ref}/*", h.GetBlob)
		r.Get("/repos/{owner}/{name}/commits", h.GetCommits)
		r.Get("/repos/{owner}/{name}/branches", h.GetBranches)
		r.Get("/repos/{owner}/{name}/readme", h.GetReadme)

		// Issues
		r.Get("/repos/{owner}/{name}/issues", h.ListIssues)
		r.Post("/repos/{owner}/{name}/issues", h.CreateIssue)
		r.Get("/repos/{owner}/{name}/issues/{number}", h.GetIssue)
		r.Put("/repos/{owner}/{name}/issues/{number}", h.UpdateIssue)
		r.Get("/repos/{owner}/{name}/issues/{number}/comments", h.ListIssueComments)
		r.Post("/repos/{owner}/{name}/issues/{number}/comments", h.CreateIssueComment)
		r.Put("/repos/{owner}/{name}/issues/{number}/comments/{id}", h.UpdateIssueComment)
		r.Delete("/repos/{owner}/{name}/issues/{number}/comments/{id}", h.DeleteIssueComment)

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

		// Shortcuts
		r.Get("/shortcuts", h.ListShortcuts)
		r.Post("/shortcuts", h.CreateShortcut)
		r.Put("/shortcuts/{id}", h.UpdateShortcut)
		r.Delete("/shortcuts/{id}", h.DeleteShortcut)
	})

	// Git Smart HTTP protocol
	r.Get("/{owner}/{repo}.git/info/refs", h.GitInfoRefs)
	r.Post("/{owner}/{repo}.git/git-upload-pack", h.GitUploadPack)
	r.Post("/{owner}/{repo}.git/git-receive-pack", h.GitReceivePack)

	// WebSocket
	r.Get("/ws", hub.HandleWebSocket)

	port := cfg.Port
	if port == "" {
		port = "8080"
	}
	log.Printf("devnook-server listening on :%s", port)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatal(err)
	}
}
