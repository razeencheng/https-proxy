package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStatsDB_InitAndUpsert(t *testing.T) {
	// Create a temp directory for test DB
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db, err := NewStatsDB(dbPath)
	if err != nil {
		t.Fatalf("NewStatsDB: %v", err)
	}
	defer db.Close()

	// Verify file exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("DB file not created")
	}

	// Batch upsert some records
	now := time.Now()
	records := []TrafficRecord{
		{
			Username:    "alice",
			Domain:      "google.com",
			Upload:      1000,
			Download:    5000,
			ConnCount:   1,
			Country:     "US",
			CountryName: "United States",
			Continent:   "NA",
			Minute:      now.Truncate(time.Minute).Format("2006-01-02T15:04:00"),
			Hour:        now.Truncate(time.Hour).Format("2006-01-02T15:00:00"),
			Timestamp:   now,
		},
		{
			Username:    "alice",
			Domain:      "github.com",
			Upload:      2000,
			Download:    8000,
			ConnCount:   1,
			Country:     "US",
			CountryName: "United States",
			Continent:   "NA",
			Minute:      now.Truncate(time.Minute).Format("2006-01-02T15:04:00"),
			Hour:        now.Truncate(time.Hour).Format("2006-01-02T15:00:00"),
			Timestamp:   now,
		},
		{
			Username:    "bob",
			Domain:      "example.jp",
			Upload:      500,
			Download:    1500,
			ConnCount:   1,
			Country:     "JP",
			CountryName: "Japan",
			Continent:   "AS",
			Minute:      now.Truncate(time.Minute).Format("2006-01-02T15:04:00"),
			Hour:        now.Truncate(time.Hour).Format("2006-01-02T15:00:00"),
			Timestamp:   now,
		},
	}

	if err := db.BatchUpsert(records); err != nil {
		t.Fatalf("BatchUpsert: %v", err)
	}

	// Test GetOverview
	overview, err := db.GetOverview()
	if err != nil {
		t.Fatalf("GetOverview: %v", err)
	}
	if overview.UserCount != 2 {
		t.Errorf("UserCount = %d, want 2", overview.UserCount)
	}
	if overview.TotalUpload != 3500 {
		t.Errorf("TotalUpload = %d, want 3500", overview.TotalUpload)
	}
	if overview.TotalDownload != 14500 {
		t.Errorf("TotalDownload = %d, want 14500", overview.TotalDownload)
	}
	if overview.DomainCount != 3 {
		t.Errorf("DomainCount = %d, want 3", overview.DomainCount)
	}
	if overview.CountryCount != 2 {
		t.Errorf("CountryCount = %d, want 2", overview.CountryCount)
	}

	// Test GetAllUsers
	users, err := db.GetAllUsers()
	if err != nil {
		t.Fatalf("GetAllUsers: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("len(users) = %d, want 2", len(users))
	}
	// Alice should be first (more traffic)
	if users[0].Username != "alice" {
		t.Errorf("users[0].Username = %s, want alice", users[0].Username)
	}
	if users[0].TotalUpload != 3000 {
		t.Errorf("alice.TotalUpload = %d, want 3000", users[0].TotalUpload)
	}

	// Test GetTopDomains
	domains, err := db.GetTopDomains(10, "")
	if err != nil {
		t.Fatalf("GetTopDomains: %v", err)
	}
	if len(domains) != 3 {
		t.Errorf("len(domains) = %d, want 3", len(domains))
	}

	// Test GetTopDomains with user filter
	domains, err = db.GetTopDomains(10, "alice")
	if err != nil {
		t.Fatalf("GetTopDomains(alice): %v", err)
	}
	if len(domains) != 2 {
		t.Errorf("len(alice domains) = %d, want 2", len(domains))
	}

	// Test GetCountryStats
	countries, err := db.GetCountryStats()
	if err != nil {
		t.Fatalf("GetCountryStats: %v", err)
	}
	if len(countries) != 2 {
		t.Errorf("len(countries) = %d, want 2", len(countries))
	}
	if countries[0].Country != "US" {
		t.Errorf("top country = %s, want US", countries[0].Country)
	}

	// Test GetTrends
	trends, err := db.GetTrends("1h")
	if err != nil {
		t.Fatalf("GetTrends: %v", err)
	}
	if len(trends) == 0 {
		t.Error("GetTrends returned 0 points, want >= 1")
	}

	// Test user disable/enable
	db.SetUserDisabled("alice", true)
	if !db.IsUserDisabled("alice") {
		t.Error("alice should be disabled")
	}
	db.SetUserDisabled("alice", false)
	if db.IsUserDisabled("alice") {
		t.Error("alice should be enabled")
	}
}

func TestStatsDB_UpsertAccumulation(t *testing.T) {
	dir := t.TempDir()
	db, err := NewStatsDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewStatsDB: %v", err)
	}
	defer db.Close()

	now := time.Now()
	minute := now.Truncate(time.Minute).Format("2006-01-02T15:04:00")
	hour := now.Truncate(time.Hour).Format("2006-01-02T15:00:00")

	// First upsert
	db.BatchUpsert([]TrafficRecord{{
		Username: "alice", Domain: "google.com", Upload: 100, Download: 200,
		ConnCount: 1, Minute: minute, Hour: hour, Timestamp: now,
	}})

	// Second upsert – should accumulate
	db.BatchUpsert([]TrafficRecord{{
		Username: "alice", Domain: "google.com", Upload: 300, Download: 400,
		ConnCount: 1, Minute: minute, Hour: hour, Timestamp: now,
	}})

	user, err := db.GetUser("alice")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if user.TotalUpload != 400 {
		t.Errorf("alice.TotalUpload = %d, want 400 (accumulated)", user.TotalUpload)
	}
	if user.TotalDownload != 600 {
		t.Errorf("alice.TotalDownload = %d, want 600 (accumulated)", user.TotalDownload)
	}
	if user.ConnCount != 2 {
		t.Errorf("alice.ConnCount = %d, want 2", user.ConnCount)
	}
}

func TestStatsCollector_AggregateAndFlush(t *testing.T) {
	dir := t.TempDir()
	db, err := NewStatsDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewStatsDB: %v", err)
	}
	defer db.Close()

	collector := NewStatsCollector(db, nil, 1) // 1 second flush interval
	defer collector.Stop()

	// Send several events
	for i := 0; i < 5; i++ {
		collector.Record(TrafficEvent{
			Username:  "alice",
			Domain:    "google.com",
			Upload:    1000,
			Download:  2000,
			Timestamp: time.Now(),
		})
	}

	// Wait for flush
	time.Sleep(2 * time.Second)

	overview, err := db.GetOverview()
	if err != nil {
		t.Fatalf("GetOverview: %v", err)
	}
	if overview.TotalUpload != 5000 {
		t.Errorf("TotalUpload = %d, want 5000", overview.TotalUpload)
	}
	if overview.TotalDownload != 10000 {
		t.Errorf("TotalDownload = %d, want 10000", overview.TotalDownload)
	}
}
