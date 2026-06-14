package main

import (
	"log"
	"net/http"
	"os"

	"arma3modmanager/internal/config"
	"arma3modmanager/internal/database"
	"arma3modmanager/internal/handlers"
	"arma3modmanager/internal/server"
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

	// Ensure addons directory exists
	if err := utils.EnsureAddonsDirectory(); err != nil {
		log.Fatal("Failed to create addons directory:", err)
	}

	if config.AppConfig.Password == "changeme" {
		log.Println("Error: Default password is still in use. Please change it in config.json")
		os.Exit(1)
	}

	mux := http.NewServeMux()

	// JSON REST API
	mux.Handle("/api/", handlers.APIHandler())

	// Embedded React SPA (with client-side routing fallback)
	mux.Handle("/", web.Handler())

	// Listen address: defaults to all interfaces on the configured port.
	// Override with IDEAL_LISTEN (e.g. "127.0.0.1:8080") to bind loopback only,
	// which avoids the OS firewall prompt during local testing.
	addr := os.Getenv("IDEAL_LISTEN")
	if addr == "" {
		addr = ":" + config.AppConfig.Port
	}

	log.Printf("Starting Arma 3 Mod Manager on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
