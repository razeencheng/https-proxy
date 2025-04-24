package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
)

// ServerConfig contains the server configuration
type ServerConfig struct {
	Port         int `json:"port"`
	Certificates struct {
		CertPath string `json:"cert_path"`
		KeyPath  string `json:"key_path"`
		CAPath   string `json:"ca_path"`
	} `json:"certificates"`
	Performance struct {
		BufferSize         int  `json:"buffer_size"`          // 缓冲区大小，以字节为单位
		TCPKeepAlive       int  `json:"tcp_keep_alive"`       // TCP KeepAlive时间，以秒为单位
		ReadBufferSize     int  `json:"read_buffer_size"`     // TCP读缓冲区大小
		WriteBufferSize    int  `json:"write_buffer_size"`    // TCP写缓冲区大小
		MaxConcurrentConns int  `json:"max_concurrent_conns"` // 最大并发连接数
		EnableCompression  bool `json:"enable_compression"`   // 是否启用压缩
		NoDelay            bool `json:"no_delay"`             // 是否禁用Nagle算法
	} `json:"performance"`
}

// ProxyConfig contains the proxy settings
type ProxyConfig struct {
	DefaultSite string `json:"default_site"`
	Enabled     bool   `json:"enabled"`
}

// StatsConfig contains statistics settings
type StatsConfig struct {
	Enabled    bool   `json:"enabled"`
	FilePath   string `json:"file_path"`
	SavePeriod int    `json:"save_period_seconds"`
}

// AdminConfig contains admin panel settings
type AdminConfig struct {
	Port       int    `json:"port"`
	Enabled    bool   `json:"enabled"`
	Language   string `json:"language"` // "en" for English, "zh" for Chinese
	Interfaces struct {
		Web bool `json:"web"`
		API bool `json:"api"`
	} `json:"interfaces"`
	Certificates *struct {
		// Admin panel can specify its own certificate configuration
		// If not specified, it will use the server's certificates
		CertPath string `json:"cert_path"`
		KeyPath  string `json:"key_path"`
		CAPath   string `json:"ca_path"`
	} `json:"certificates,omitempty"`
}

// Config represents the application configuration
type Config struct {
	Server ServerConfig `json:"server"`
	Proxy  ProxyConfig  `json:"proxy"`
	Stats  StatsConfig  `json:"stats"`
	Admin  AdminConfig  `json:"admin"`
}

// LoadConfig loads the configuration from a file
func LoadConfig() (*Config, error) {
	configPath := flag.String("config", "config.json", "Path to configuration file")
	help := flag.Bool("help", false, "Show help")
	showVersion := flag.Bool("version", false, "Show version")
	statsEnabled := flag.Bool("stats", false, "Enable statistics collection (overrides config file)")
	statsPath := flag.String("stats-path", "", "Path to statistics file (overrides config file)")
	serverPort := flag.Int("port", 0, "Server port (overrides config file)")
	adminEnabled := flag.Bool("admin", false, "Enable admin panel (overrides config file)")
	adminPort := flag.Int("admin-port", 0, "Admin panel port (overrides config file)")
	language := flag.String("language", "", "Admin panel language (en/zh)")

	flag.Parse()

	if *help {
		flag.Usage()
		os.Exit(0)
	}

	if *showVersion {
		fmt.Println("HTTPS Proxy version 1.0.0")
		os.Exit(0)
	}

	// Load the configuration file
	data, err := os.ReadFile(*configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %v", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %v", err)
	}

	// Override configuration with command line arguments if provided
	if *serverPort > 0 {
		cfg.Server.Port = *serverPort
	}

	if *statsEnabled {
		cfg.Stats.Enabled = true
	}

	if *statsPath != "" {
		cfg.Stats.FilePath = *statsPath
	}

	if *adminEnabled {
		cfg.Admin.Enabled = true
	}

	if *adminPort > 0 {
		cfg.Admin.Port = *adminPort
	}

	if *language != "" {
		cfg.Admin.Language = *language
	}

	// Set default values if not specified
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8443
	}

	// Admin panel default settings
	if cfg.Admin.Enabled && cfg.Admin.Port == 0 {
		cfg.Admin.Port = 9444 // Default port 9444
	}

	if cfg.Admin.Enabled && !cfg.Admin.Interfaces.Web && !cfg.Admin.Interfaces.API {
		// Default enable web interface
		cfg.Admin.Interfaces.Web = true
	}

	// Set default language to English if not specified
	if cfg.Admin.Language == "" {
		cfg.Admin.Language = "en"
	}

	return &cfg, nil
}

// GetAdminCertificates returns certificate paths for admin panel
func (cfg *Config) GetAdminCertificates() (certPath, keyPath, caPath string) {
	// If admin panel doesn't have its own certificate configuration, use the server's
	if cfg.Admin.Certificates == nil || cfg.Admin.Certificates.CertPath == "" {
		return cfg.Server.Certificates.CertPath, cfg.Server.Certificates.KeyPath, cfg.Server.Certificates.CAPath
	}
	return cfg.Admin.Certificates.CertPath, cfg.Admin.Certificates.KeyPath, cfg.Admin.Certificates.CAPath
}

// SaveConfig saves the configuration to a file
func (cfg *Config) SaveConfig(path string) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode config: %v", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %v", err)
	}

	log.Printf("Configuration saved to %s", path)
	return nil
}
