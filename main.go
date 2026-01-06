package main

import (
	"log"
	"net/http"
	"os"

	"arma3modmanager/internal/auth"
	"arma3modmanager/internal/config"
	"arma3modmanager/internal/database"
	"arma3modmanager/internal/handlers"
	"arma3modmanager/internal/utils"
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

	// Ensure addons directory exists
	if err := utils.EnsureAddonsDirectory(); err != nil {
		log.Fatal("Failed to create addons directory:", err)
	}

	// Setup routes - Login/Logout routes don't require auth
	http.HandleFunc("/login", handlers.LoginHandler)
	http.HandleFunc("/logout", handlers.LogoutHandler)

	// Protected routes
	http.HandleFunc("/", auth.RequireAuth(handlers.IndexHandler))
	http.HandleFunc("/settings", auth.RequireAuth(handlers.SettingsHandler))
	http.HandleFunc("/settings/save", auth.RequireAuth(handlers.SaveSettingsHandler))
	http.HandleFunc("/modsets", auth.RequireAuth(handlers.ModsetsHandler))
	http.HandleFunc("/modsets/upload", auth.RequireAuth(handlers.UploadModsetHandler))
	http.HandleFunc("/modsets/delete", auth.RequireAuth(handlers.DeleteModsetHandler))
	http.HandleFunc("/mods", auth.RequireAuth(handlers.ModsHandler))
	http.HandleFunc("/mods/add", auth.RequireAuth(handlers.AddModHandler))
	http.HandleFunc("/mods/check-updates", auth.RequireAuth(handlers.CheckUpdatesHandler))
	http.HandleFunc("/mods/generate-a3sync", auth.RequireAuth(handlers.GenerateA3SyncHandler))
	http.HandleFunc("/mods/download", auth.RequireAuth(handlers.DownloadModHandler))
	http.HandleFunc("/mods/download/progress", auth.RequireAuth(handlers.DownloadProgressHandler))
	http.HandleFunc("/mods/delete", auth.RequireAuth(handlers.DeleteModHandler))
	http.HandleFunc("/startup-command", auth.RequireAuth(handlers.StartupCommandHandler))

	// Serve static files
	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	port := ":" + config.AppConfig.Port
	
	if config.AppConfig.Password == "changeme" {
		log.Println("Error: Default password is still in use. Please change it in config.json")
		os.Exit(1)
	}

	log.Printf("Starting Arma 3 Mod Manager on %s", port)
	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatal(err)
	}
}
