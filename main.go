package main

import (
	"log"
	"net/http"
	"os"

	"arma3modmanager/internal/auth"
	"arma3modmanager/internal/automation"
	"arma3modmanager/internal/config"
	"arma3modmanager/internal/database"
	"arma3modmanager/internal/handlers"
	"arma3modmanager/internal/server"
	"arma3modmanager/internal/steam"
	"arma3modmanager/internal/updatecheck"
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

	auth.StartSessionCleanup()

	server.Init()

	updatecheck.Default.Start()

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

	// Public mission downloads live under /dl/ so server slugs cannot collide with
	// SPA routes (/servers, /login, …) or /api/.
	missions := http.NewServeMux()
	missions.HandleFunc("GET /dl/{slug}/missions/{name}", handlers.PublicMissionDownloadHandler)

	app := http.NewServeMux()
	app.Handle("/api/", handlers.APIHandler())
	app.Handle("/", web.Handler())

	root := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
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

	// Ideal is built with MPTCP disabled (godebug multipathtcp=0 in go.mod), so its
	// own listeners are plain TCP. Still warn when the host has MPTCP enabled, as
	// an unpatched kernel exposes any MPTCP listener to the CVE-2026-98432 exploit.
	if utils.HostMPTCPEnabled() {
		log.Printf("[IDEAL] WARNING: host has net.mptcp.enabled=1. If the kernel is not patched for\n" +
			"  CVE-2026-98432 (IPv4 TCP use-after-free; fixed in 5.10.272 / 5.15.223 / 6.1.190 /\n" +
			"  6.6.159 / 6.12.113 / 6.18.56 / 7.2.10+), MPTCP listeners on this host are exposed to\n" +
			"  the demonstrated exploit. Ideal itself is built with MPTCP disabled (godebug multipathtcp=0).")
	}

	// Last, so nothing is started by a run that's about to exit over bad settings.
	automation.Default.Start()

	log.Printf("Starting Arma 3 Mod Manager on %s", addr)
	if err := http.ListenAndServe(addr, root); err != nil {
		log.Fatal(err)
	}
}
