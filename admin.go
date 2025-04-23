package main

import (
	"crypto/tls"
	"crypto/x509"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

//go:embed templates/*.html
var templatesFS embed.FS

// AdminServer represents the admin panel server
type AdminServer struct {
	Config       *Config
	StatsManager *StatsManager
	Server       *http.Server
	Templates    *template.Template
	CACertPool   *x509.CertPool
}

// NewAdminServer creates a new admin panel server
func NewAdminServer(config *Config, statsManager *StatsManager) (*AdminServer, error) {
	if !config.Admin.Enabled {
		return nil, nil
	}

	// Prepare template functions
	funcMap := template.FuncMap{
		"add": func(a, b interface{}) uint64 {
			// Convert parameters to uint64 to support various types in templates
			var aVal, bVal uint64

			switch v := a.(type) {
			case int:
				aVal = uint64(v)
			case int64:
				aVal = uint64(v)
			case uint64:
				aVal = v
			default:
				// Default to 0
				aVal = 0
			}

			switch v := b.(type) {
			case int:
				bVal = uint64(v)
			case int64:
				bVal = uint64(v)
			case uint64:
				bVal = v
			default:
				// Default to 0
				bVal = 0
			}

			return aVal + bVal
		},
		"div": func(a, b uint64) uint64 { return uint64(float64(a) / float64(b)) },
		"max": func(a, b uint64) uint64 {
			if a > b {
				return a
			} else {
				return b
			}
		},
		"timeElapsed": calculateTimeElapsed,
		"formatBytes": formatBytes,
	}

	// Parse templates
	templates, err := template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("failed to parse templates: %v", err)
	}

	// Get certificate configuration
	certPath, keyPath, caPath := config.GetAdminCertificates()

	// Load server certificate
	serverCert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load admin certificate and key: %v", err)
	}

	// Load CA certificate
	caCert, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load admin CA certificate: %v", err)
	}

	// Create CA certificate pool
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse admin CA certificate")
	}

	// Create admin panel server
	adminServer := &AdminServer{
		Config:       config,
		StatsManager: statsManager,
		Templates:    templates,
		CACertPool:   caCertPool,
	}

	// Create routes
	mux := http.NewServeMux()

	// API routes
	if config.Admin.Interfaces.API {
		mux.HandleFunc("/api/stats", adminServer.handleAPIStats)
		mux.HandleFunc("/api/stats/user/", adminServer.handleAPIUserStats)
		mux.HandleFunc("/api/config", adminServer.handleAPIConfig)
		mux.HandleFunc("/api/user/enable/", adminServer.handleAPIEnableUser)
		mux.HandleFunc("/api/user/disable/", adminServer.handleAPIDisableUser)
	}

	// Web interface routes
	if config.Admin.Interfaces.Web {
		mux.HandleFunc("/", adminServer.handleHome)
		mux.HandleFunc("/user/", adminServer.handleUserDetail)
		mux.HandleFunc("/assets/", adminServer.handleAssets)
	}

	// Create HTTPS server
	server := &http.Server{
		Addr: ":" + strconv.Itoa(config.Admin.Port),
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{serverCert},
			ClientCAs:    caCertPool,
			ClientAuth:   tls.RequireAndVerifyClientCert, // Require and verify client certificates
			MinVersion:   tls.VersionTLS12,               // Minimum TLS 1.2
		},
		Handler: mux,
	}

	adminServer.Server = server

	return adminServer, nil
}

// calculateTimeElapsed is a helper function to calculate time differences
func calculateTimeElapsed(startTime time.Time) string {
	now := time.Now()
	diff := now.Sub(startTime)

	days := int(diff.Hours() / 24)
	hours := int(diff.Hours()) % 24
	minutes := int(diff.Minutes()) % 60

	// Note: The language is determined in the template based on the current language setting
	result := ""
	if days > 0 {
		result += fmt.Sprintf("%d days ", days) // In template: {{days}} {{if eq .Language "en"}}days{{else}}天{{end}}
	}
	if hours > 0 || days > 0 {
		result += fmt.Sprintf("%d hours ", hours) // In template: {{hours}} {{if eq .Language "en"}}hours{{else}}小时{{end}}
	}
	result += fmt.Sprintf("%d minutes", minutes) // In template: {{minutes}} {{if eq .Language "en"}}minutes{{else}}分钟{{end}}

	return result
}

// Start starts the admin panel server
func (a *AdminServer) Start() {
	if a == nil || !a.Config.Admin.Enabled {
		return
	}

	// Start the server in a separate goroutine
	go func() {
		log.Printf("Starting admin panel server on port %d...\n", a.Config.Admin.Port)
		if err := a.Server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			log.Printf("Admin panel server error: %v\n", err)
		}
	}()
}

// Stop stops the admin panel server
func (a *AdminServer) Stop() {
	if a == nil || a.Server == nil {
		return
	}

	log.Println("Stopping admin panel server...")
	if err := a.Server.Close(); err != nil {
		log.Printf("Error closing admin panel server: %v", err)
	}
}

// pageData is the data passed to templates
type pageData struct {
	Title        string
	LastUpdated  time.Time
	Users        map[string]*UserStats
	SelectedUser *UserStats
	Config       *Config
	Language     string
	FormatBytes  func(uint64) string
}

// formatBytes formats bytes into a human-readable form
func formatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}

	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.2f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// WebResponse API response format
type WebResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data"`
	Error   string      `json:"error,omitempty"`
}

// isAdmin verifies if the request is from an admin
func (a *AdminServer) isAdmin(r *http.Request) bool {
	// All users with valid client certificates are considered admins
	// because we've already set client certificate verification in TLS config
	return r.TLS != nil && len(r.TLS.PeerCertificates) > 0
}

// Web API handlers

// handleHome renders the homepage
func (a *AdminServer) handleHome(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Check for language toggle
	if r.URL.Query().Get("lang") != "" {
		lang := r.URL.Query().Get("lang")
		if lang == "en" || lang == "zh" {
			a.Config.Admin.Language = lang
		}
	}

	// Prepare page data
	data := pageData{
		Title:       "HTTPS Proxy - Admin Panel",
		LastUpdated: time.Now(),
		Users:       a.StatsManager.GetUserStats(),
		Config:      a.Config,
		Language:    a.Config.Admin.Language,
		FormatBytes: formatBytes,
	}

	// Render template
	if err := a.Templates.ExecuteTemplate(w, "dashboard.html", data); err != nil {
		log.Printf("Error rendering template: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// handleUserDetail renders the user detail page
func (a *AdminServer) handleUserDetail(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Check for language toggle
	if r.URL.Query().Get("lang") != "" {
		lang := r.URL.Query().Get("lang")
		if lang == "en" || lang == "zh" {
			a.Config.Admin.Language = lang
		}
	}

	// Get username from URL
	username := r.URL.Path[len("/user/"):]
	if username == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	// Get user statistics
	user := a.StatsManager.GetUserStatsByName(username)
	if user == nil {
		http.NotFound(w, r)
		return
	}

	// Prepare page data
	data := pageData{
		Title:        fmt.Sprintf("User Details - %s", username),
		LastUpdated:  time.Now(),
		Users:        a.StatsManager.GetUserStats(),
		SelectedUser: user,
		Config:       a.Config,
		Language:     a.Config.Admin.Language,
		FormatBytes:  formatBytes,
	}

	// Render template
	if err := a.Templates.ExecuteTemplate(w, "user_detail.html", data); err != nil {
		log.Printf("Error rendering template: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// handleAssets serves static assets
func (a *AdminServer) handleAssets(w http.ResponseWriter, r *http.Request) {
	// Handle CSS, JS, etc. static resources
	// Since our implementation is embedded, we don't need to separately handle static resources
	http.NotFound(w, r)
}

// REST API handlers

// handleAPIStats returns statistics for all users
func (a *AdminServer) handleAPIStats(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		writeJSONResponse(w, WebResponse{Success: false, Error: "Unauthorized"}, http.StatusUnauthorized)
		return
	}

	writeJSONResponse(w, WebResponse{
		Success: true,
		Data:    a.StatsManager.GetUserStats(),
	}, http.StatusOK)
}

// handleAPIUserStats returns statistics for a specific user
func (a *AdminServer) handleAPIUserStats(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		writeJSONResponse(w, WebResponse{Success: false, Error: "Unauthorized"}, http.StatusUnauthorized)
		return
	}

	// Get username from URL
	username := r.URL.Path[len("/api/stats/user/"):]
	if username == "" {
		writeJSONResponse(w, WebResponse{Success: false, Error: "Username required"}, http.StatusBadRequest)
		return
	}

	// Get user statistics
	user := a.StatsManager.GetUserStatsByName(username)
	if user == nil {
		writeJSONResponse(w, WebResponse{Success: false, Error: "User not found"}, http.StatusNotFound)
		return
	}

	writeJSONResponse(w, WebResponse{
		Success: true,
		Data:    user,
	}, http.StatusOK)
}

// handleAPIConfig returns server configuration
func (a *AdminServer) handleAPIConfig(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		writeJSONResponse(w, WebResponse{Success: false, Error: "Unauthorized"}, http.StatusUnauthorized)
		return
	}

	// For security reasons, only return partial configuration
	safeConfig := struct {
		ServerPort      int  `json:"server_port"`
		AdminPort       int  `json:"admin_port"`
		StatsEnabled    bool `json:"stats_enabled"`
		StatsSavePeriod int  `json:"stats_save_period"`
	}{
		ServerPort:      a.Config.Server.Port,
		AdminPort:       a.Config.Admin.Port,
		StatsEnabled:    a.Config.Stats.Enabled,
		StatsSavePeriod: a.Config.Stats.SavePeriod,
	}

	writeJSONResponse(w, WebResponse{
		Success: true,
		Data:    safeConfig,
	}, http.StatusOK)
}

// handleAPIEnableUser enables a specified user
func (a *AdminServer) handleAPIEnableUser(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		writeJSONResponse(w, WebResponse{Success: false, Error: "Unauthorized"}, http.StatusUnauthorized)
		return
	}

	if r.Method != "POST" {
		writeJSONResponse(w, WebResponse{Success: false, Error: "Method not allowed"}, http.StatusMethodNotAllowed)
		return
	}

	// Get username from URL
	username := r.URL.Path[len("/api/user/enable/"):]
	if username == "" {
		writeJSONResponse(w, WebResponse{Success: false, Error: "Username required"}, http.StatusBadRequest)
		return
	}

	success := a.StatsManager.EnableUser(username)
	writeJSONResponse(w, WebResponse{
		Success: true,
		Data: map[string]interface{}{
			"username": username,
			"enabled":  true,
			"changed":  success,
		},
	}, http.StatusOK)
}

// handleAPIDisableUser disables a specified user
func (a *AdminServer) handleAPIDisableUser(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		writeJSONResponse(w, WebResponse{Success: false, Error: "Unauthorized"}, http.StatusUnauthorized)
		return
	}

	if r.Method != "POST" {
		writeJSONResponse(w, WebResponse{Success: false, Error: "Method not allowed"}, http.StatusMethodNotAllowed)
		return
	}

	// Get username from URL
	username := r.URL.Path[len("/api/user/disable/"):]
	if username == "" {
		writeJSONResponse(w, WebResponse{Success: false, Error: "Username required"}, http.StatusBadRequest)
		return
	}

	success := a.StatsManager.DisableUser(username)
	writeJSONResponse(w, WebResponse{
		Success: true,
		Data: map[string]interface{}{
			"username": username,
			"enabled":  false,
			"changed":  success,
		},
	}, http.StatusOK)
}

// writeJSONResponse writes data as JSON to the response
func writeJSONResponse(w http.ResponseWriter, data interface{}, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Error encoding JSON response: %v", err)
	}
}
