package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// UserStats represents statistics for a single user
type UserStats struct {
	Username        string    `json:"username"`
	TotalBytes      uint64    `json:"total_bytes"`
	LastAccess      time.Time `json:"last_access"`
	RequestsCount   uint64    `json:"requests_count"`
	ConnectedSince  time.Time `json:"connected_since,omitempty"`
	ConnectionCount uint64    `json:"connection_count"`
	Disabled        bool      `json:"disabled"`
}

// StatsManager manages user statistics
type StatsManager struct {
	sync.RWMutex
	Config     *Config
	UserStats  map[string]*UserStats
	ticker     *time.Ticker
	done       chan bool
	dirtyStats bool // Flag indicating if there have been changes since last save
}

// NewStatsManager creates a new statistics manager
func NewStatsManager(config *Config) *StatsManager {
	sm := &StatsManager{
		Config:    config,
		UserStats: make(map[string]*UserStats),
		done:      make(chan bool),
	}

	if config.Stats.Enabled {
		// Try to load previous statistics from file
		sm.loadStats()

		// Start periodic saving
		if config.Stats.SavePeriod > 0 {
			sm.ticker = time.NewTicker(time.Duration(config.Stats.SavePeriod) * time.Second)
			go sm.periodicSave()
		}
	}

	return sm
}

// RecordTraffic records traffic for a user
func (sm *StatsManager) RecordTraffic(username string, bytesCount uint64) {
	if !sm.Config.Stats.Enabled {
		return
	}

	sm.Lock()
	defer sm.Unlock()

	sm.dirtyStats = true

	// Get or create user statistics
	stats, ok := sm.UserStats[username]
	if !ok {
		stats = &UserStats{
			Username:       username,
			ConnectedSince: time.Now(),
		}
		sm.UserStats[username] = stats
	}

	// Update statistics
	stats.TotalBytes += bytesCount
	stats.LastAccess = time.Now()
}

// RecordRequest records a request for a user
func (sm *StatsManager) RecordRequest(username string) {
	if !sm.Config.Stats.Enabled {
		return
	}

	sm.Lock()
	defer sm.Unlock()

	sm.dirtyStats = true

	// Get or create user statistics
	stats, ok := sm.UserStats[username]
	if !ok {
		stats = &UserStats{
			Username:       username,
			ConnectedSince: time.Now(),
		}
		sm.UserStats[username] = stats
	}

	// Update statistics
	stats.RequestsCount++
	stats.LastAccess = time.Now()
}

// RecordConnection records a connection for a user
func (sm *StatsManager) RecordConnection(username string) {
	if !sm.Config.Stats.Enabled {
		return
	}

	sm.Lock()
	defer sm.Unlock()

	sm.dirtyStats = true

	// Get or create user statistics
	stats, ok := sm.UserStats[username]
	if !ok {
		stats = &UserStats{
			Username:       username,
			ConnectedSince: time.Now(),
		}
		sm.UserStats[username] = stats
	}

	// Update statistics
	stats.ConnectionCount++
	stats.LastAccess = time.Now()
}

// GetUserStats returns a map of all user statistics
func (sm *StatsManager) GetUserStats() map[string]*UserStats {
	sm.RLock()
	defer sm.RUnlock()

	// Create a copy to prevent external modifications
	result := make(map[string]*UserStats, len(sm.UserStats))
	for k, v := range sm.UserStats {
		result[k] = &UserStats{
			Username:        v.Username,
			TotalBytes:      v.TotalBytes,
			LastAccess:      v.LastAccess,
			RequestsCount:   v.RequestsCount,
			ConnectedSince:  v.ConnectedSince,
			ConnectionCount: v.ConnectionCount,
			Disabled:        v.Disabled,
		}
	}
	return result
}

// GetUserStatsByName returns statistics for a specific user
func (sm *StatsManager) GetUserStatsByName(username string) *UserStats {
	sm.RLock()
	defer sm.RUnlock()

	stats, ok := sm.UserStats[username]
	if !ok {
		return nil
	}

	// Return a copy to prevent external modifications
	return &UserStats{
		Username:        stats.Username,
		TotalBytes:      stats.TotalBytes,
		LastAccess:      stats.LastAccess,
		RequestsCount:   stats.RequestsCount,
		ConnectedSince:  stats.ConnectedSince,
		ConnectionCount: stats.ConnectionCount,
		Disabled:        stats.Disabled,
	}
}

// periodicSave periodically saves statistics to a file
func (sm *StatsManager) periodicSave() {
	for {
		select {
		case <-sm.ticker.C:
			sm.SaveStats()
		case <-sm.done:
			return
		}
	}
}

// SaveStats saves user statistics to a file
func (sm *StatsManager) SaveStats() {
	if !sm.Config.Stats.Enabled || sm.Config.Stats.FilePath == "" {
		return
	}

	sm.Lock()
	defer sm.Unlock()

	// Mark as not dirty before saving
	sm.dirtyStats = false

	filePath := sm.Config.Stats.FilePath
	dirPath := filepath.Dir(filePath)

	// Ensure directory exists
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		log.Printf("Failed to create directory for stats file: %v", err)
		return
	}

	// Encode statistics as JSON
	data, err := json.MarshalIndent(sm.UserStats, "", "  ")
	if err != nil {
		log.Printf("Failed to encode stats data: %v", err)
		return
	}

	// Write to file
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		log.Printf("Failed to write stats file: %v", err)
		return
	}

	log.Printf("Statistics saved to %s", filePath)
}

// loadStats loads user statistics from a file
func (sm *StatsManager) loadStats() {
	if !sm.Config.Stats.Enabled || sm.Config.Stats.FilePath == "" {
		return
	}

	filePath := sm.Config.Stats.FilePath

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		log.Printf("Stats file does not exist, starting with empty stats")
		return
	}

	// Read file
	data, err := os.ReadFile(filePath)
	if err != nil {
		log.Printf("Failed to read stats file: %v", err)
		return
	}

	// Decode JSON data
	var stats map[string]*UserStats
	if err := json.Unmarshal(data, &stats); err != nil {
		log.Printf("Failed to decode stats data: %v", err)
		return
	}

	sm.UserStats = stats
	log.Printf("Statistics loaded from %s", filePath)
}

// Stop stops the statistics manager and saves data if needed
func (sm *StatsManager) Stop() {
	if sm.Config.Stats.Enabled {
		sm.SaveStats() // Save one last time before exiting
	}

	if sm.ticker != nil {
		sm.ticker.Stop()
		sm.done <- true
	}
}

// UserTrafficReader is an io.Reader that tracks traffic for a user
type UserTrafficReader struct {
	r        *StatsManager
	username string
	reader   *CountingReader
}

// NewUserTrafficReader creates a new user traffic tracking Reader
func NewUserTrafficReader(r *StatsManager, username string, reader *CountingReader) *UserTrafficReader {
	return &UserTrafficReader{
		r:        r,
		username: username,
		reader:   reader,
	}
}

// Read reads data and tracks traffic
func (r *UserTrafficReader) Read(p []byte) (n int, err error) {
	n, err = r.reader.Read(p)
	if n > 0 {
		r.r.RecordTraffic(r.username, uint64(n))
	}
	return
}

// Close closes the reader
func (r *UserTrafficReader) Close() error {
	if closer, ok := r.reader.reader.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

// UserTrafficWriter is an io.Writer that tracks traffic for a user
type UserTrafficWriter struct {
	r        *StatsManager
	username string
	writer   *CountingWriter
}

// NewUserTrafficWriter creates a new user traffic tracking Writer
func NewUserTrafficWriter(r *StatsManager, username string, writer *CountingWriter) *UserTrafficWriter {
	return &UserTrafficWriter{
		r:        r,
		username: username,
		writer:   writer,
	}
}

// Write writes data and tracks traffic
func (w *UserTrafficWriter) Write(p []byte) (n int, err error) {
	n, err = w.writer.Write(p)
	if n > 0 {
		w.r.RecordTraffic(w.username, uint64(n))
	}
	return
}

// Close closes the writer
func (w *UserTrafficWriter) Close() error {
	if closer, ok := w.writer.writer.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

// CountingReader is a Reader that counts bytes read
type CountingReader struct {
	reader    interface{ Read([]byte) (int, error) }
	bytesRead uint64
}

// NewCountingReader creates a new counting Reader
func NewCountingReader(reader interface{ Read([]byte) (int, error) }) *CountingReader {
	return &CountingReader{reader: reader}
}

// Read reads data and counts bytes
func (r *CountingReader) Read(p []byte) (n int, err error) {
	n, err = r.reader.Read(p)
	r.bytesRead += uint64(n)
	return
}

// BytesRead returns the number of bytes read
func (r *CountingReader) BytesRead() uint64 {
	return r.bytesRead
}

// CountingWriter is a Writer that counts bytes written
type CountingWriter struct {
	writer       interface{ Write([]byte) (int, error) }
	bytesWritten uint64
}

// NewCountingWriter creates a new counting Writer
func NewCountingWriter(writer interface{ Write([]byte) (int, error) }) *CountingWriter {
	return &CountingWriter{writer: writer}
}

// Write writes data and counts bytes
func (w *CountingWriter) Write(p []byte) (n int, err error) {
	n, err = w.writer.Write(p)
	w.bytesWritten += uint64(n)
	return
}

// BytesWritten returns the number of bytes written
func (w *CountingWriter) BytesWritten() uint64 {
	return w.bytesWritten
}

// PrintStats prints statistics for debugging
func (sm *StatsManager) PrintStats() {
	sm.RLock()
	defer sm.RUnlock()

	fmt.Println("Current User Statistics:")
	for _, stats := range sm.UserStats {
		fmt.Printf("User: %s\n", stats.Username)
		fmt.Printf("  Total Bytes: %d\n", stats.TotalBytes)
		fmt.Printf("  Requests: %d\n", stats.RequestsCount)
		fmt.Printf("  Connections: %d\n", stats.ConnectionCount)
		fmt.Printf("  Last Access: %s\n", stats.LastAccess.Format(time.RFC3339))
		fmt.Printf("  Connected Since: %s\n", stats.ConnectedSince.Format(time.RFC3339))
		fmt.Println()
	}
}

// IsUserDisabled checks if a user is disabled
func (sm *StatsManager) IsUserDisabled(username string) bool {
	sm.RLock()
	defer sm.RUnlock()

	stats, ok := sm.UserStats[username]
	if !ok {
		return false // User does not exist, defaults to not disabled
	}

	return stats.Disabled
}

// DisableUser disables a specified user
func (sm *StatsManager) DisableUser(username string) bool {
	sm.Lock()
	defer sm.Unlock()

	sm.dirtyStats = true

	stats, ok := sm.UserStats[username]
	if !ok {
		// User does not exist, create one and disable it
		stats = &UserStats{
			Username:       username,
			ConnectedSince: time.Now(),
			Disabled:       true,
		}
		sm.UserStats[username] = stats
		return true
	}

	// Already disabled, return false
	if stats.Disabled {
		return false
	}

	stats.Disabled = true
	return true
}

// EnableUser enables a specified user
func (sm *StatsManager) EnableUser(username string) bool {
	sm.Lock()
	defer sm.Unlock()

	sm.dirtyStats = true

	stats, ok := sm.UserStats[username]
	if !ok {
		// User does not exist, create one and enable it
		stats = &UserStats{
			Username:       username,
			ConnectedSince: time.Now(),
			Disabled:       false,
		}
		sm.UserStats[username] = stats
		return true
	}

	// Already enabled, return false
	if !stats.Disabled {
		return false
	}

	stats.Disabled = false
	return true
}
