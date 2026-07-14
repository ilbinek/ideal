package main

import (
	"log"
	"net/http"
	"os"
	"strings"

	"arma3modmanager/internal/config"
	"arma3modmanager/internal/database"
	"arma3modmanager/internal/handlers"
	"arma3modmanager/internal/server"
	"arma3modmanager/internal/steam"
	"arma3modmanager/internal/utils"
	"arma3modmanager/web"
)

func main() {
	// Load configuration
	if err := config.Load(); err != nil {
		log.Fatal("Failed to load configuration:", err)
	}

	// Initialize database
	if err := database.Init(); err != nil {
		log.Fatal("Failed to initialize database:", err)
	}
	defer database.Close()

	server.Init()

	log.Printf("[IDEAL] data directory: %s", utils.DataRoot())

	// Initialize the built-in Steam (gosteam) token/state store.
	if err := steam.Default.Init(); err != nil {
		log.Fatal("Failed to initialize Steam store:", err)
	}

	// Ensure addons directory exists
	if err := utils.EnsureAddonsDirectory(); err != nil {
		log.Fatal("Failed to create addons directory:", err)
	}

	if config.AppConfig.Password == "changeme" {
		log.Println("Error: Default password is still in use. Please change it in config.json")
		os.Exit(1)
	}

	// Public mission downloads use /{slug}/missions/{name}, which cannot share a
	// ServeMux with /api/ (Go treats {slug}=api as overlapping). Route them on a
	// separate mux and only consult it for non-/api/ GET requests.
	missions := http.NewServeMux()
	missions.HandleFunc("GET /{slug}/missions/{name}", handlers.PublicMissionDownloadHandler)

	app := http.NewServeMux()
	app.Handle("/api/", handlers.APIHandler())
	app.Handle("/", web.Handler())

	root := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && !strings.HasPrefix(r.URL.Path, "/api/") {
			// Handler() only checks a match; ServeHTTP is required so PathValue
			// wildcards ({slug}, {name}) are populated on the request.
			if _, pattern := missions.Handler(r); pattern != "" {
				missions.ServeHTTP(w, r)
				return
			}
		}
		app.ServeHTTP(w, r)
	})

	// Listen address: defaults to all interfaces on the configured port.
	// Override with IDEAL_LISTEN (e.g. "127.0.0.1:8080") to bind loopback only,
	// which avoids the OS firewall prompt during local testing.
	addr := os.Getenv("IDEAL_LISTEN")
	if addr == "" {
		addr = ":" + config.AppConfig.Port
	}

	log.Printf("Starting Arma 3 Mod Manager on %s", addr)
	if err := http.ListenAndServe(addr, root); err != nil {
		log.Fatal(err)
	}
}
