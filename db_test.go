package main

import (
	"encoding/json"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStatsDB_InitAndUpsert(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db, err := NewStatsDB(dbPath)
	if err != nil {
		t.Fatalf("NewStatsDB: %v", err)
	}
	defer db.Close()

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("DB file not created")
	}

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

	// Test user disable/enable (in-memory cache backed by SQLite)
	if err := db.SetUserDisabled("alice", true); err != nil {
		t.Fatalf("SetUserDisabled(true): %v", err)
	}
	if !db.IsUserDisabled("alice") {
		t.Error("alice should be disabled")
	}
	if err := db.SetUserDisabled("alice", false); err != nil {
		t.Fatalf("SetUserDisabled(false): %v", err)
	}
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

	db.BatchUpsert([]TrafficRecord{{
		Username: "alice", Domain: "google.com", Upload: 100, Download: 200,
		ConnCount: 1, Minute: minute, Hour: hour, Timestamp: now,
	}})

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

	collector := NewStatsCollector(db, nil, 1)

	for i := 0; i < 5; i++ {
		collector.Record(TrafficEvent{
			Username:  "alice",
			Domain:    "google.com",
			Upload:    1000,
			Download:  2000,
			Timestamp: time.Now(),
		})
	}
	collector.RecordRequest("alice")
	collector.RecordRequest("alice")

	// Stop flushes remaining events deterministically
	collector.Stop()

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
	if overview.TotalConns != 5 {
		t.Errorf("TotalConns = %d, want 5", overview.TotalConns)
	}

	user, err := db.GetUser("alice")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if user.RequestCount != 2 {
		t.Errorf("RequestCount = %d, want 2", user.RequestCount)
	}
}

func TestStatsDB_DailyRangeQueriesAndRollingTrends(t *testing.T) {
	dir := t.TempDir()
	db, err := NewStatsDB(filepath.Join(dir, "range.db"))
	if err != nil {
		t.Fatalf("NewStatsDB: %v", err)
	}
	defer db.Close()

	now := time.Now()
	today := now.Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
	tenDaysAgo := now.AddDate(0, 0, -10).Format("2006-01-02")

	records := []TrafficRecord{
		{
			Username: "alice", Domain: "github.com", Country: "US", CountryName: "United States", Continent: "NA",
			Upload: 1000, Download: 4000, ConnCount: 2,
			Date: today, Hour: today + "T10:00:00", Minute: today + "T10:05:00", Timestamp: now,
		},
		{
			Username: "bob", Domain: "github.com", Country: "US", CountryName: "United States", Continent: "NA",
			Upload: 500, Download: 1500, ConnCount: 1,
			Date: today, Hour: today + "T11:00:00", Minute: today + "T11:05:00", Timestamp: now,
		},
		{
			Username: "alice", Domain: "youtube.com", Country: "JP", CountryName: "Japan", Continent: "AS",
			Upload: 200, Download: 8000, ConnCount: 3,
			Date: yesterday, Hour: yesterday + "T14:00:00", Minute: yesterday + "T14:05:00", Timestamp: now.AddDate(0, 0, -1),
		},
		{
			Username: "charlie", Domain: "old.example.com", Country: "DE", CountryName: "Germany", Continent: "EU",
			Upload: 300, Download: 700, ConnCount: 1,
			Date: tenDaysAgo, Hour: tenDaysAgo + "T09:00:00", Minute: tenDaysAgo + "T09:05:00", Timestamp: now.AddDate(0, 0, -10),
		},
	}

	if err := db.BatchUpsert(records); err != nil {
		t.Fatalf("BatchUpsert: %v", err)
	}

	// Single day overview (today)
	ovToday, err := db.GetOverviewRange(today, today, "")
	if err != nil {
		t.Fatalf("GetOverviewRange(today): %v", err)
	}
	if ovToday.TotalUpload != 1500 || ovToday.TotalDownload != 5500 || ovToday.TotalConns != 3 || ovToday.DomainCount != 1 || ovToday.CountryCount != 1 {
		t.Fatalf("unexpected today overview: %+v", ovToday)
	}

	// Filtered by user (alice over last 7 days)
	weekStart := now.AddDate(0, 0, -6).Format("2006-01-02")
	ovAliceWeek, err := db.GetOverviewRange(weekStart, today, "alice")
	if err != nil {
		t.Fatalf("GetOverviewRange(alice, 7d): %v", err)
	}
	if ovAliceWeek.TotalUpload != 1200 || ovAliceWeek.TotalDownload != 12000 || ovAliceWeek.DomainCount != 2 || ovAliceWeek.CountryCount != 2 {
		t.Fatalf("unexpected alice 7d overview: %+v", ovAliceWeek)
	}

	// Top domains aggregated across users on today (alice + bob both hit github.com)
	doms, err := db.GetTopDomainsRange(today, today, 10, "", "")
	if err != nil {
		t.Fatalf("GetTopDomainsRange: %v", err)
	}
	if len(doms) != 1 || doms[0].Domain != "github.com" || doms[0].Upload != 1500 || doms[0].Download != 5500 {
		t.Fatalf("expected aggregated github.com row, got %+v", doms)
	}

	// Domain search
	domsSearch, err := db.GetTopDomainsRange(weekStart, today, 10, "", "tube")
	if err != nil {
		t.Fatalf("GetTopDomainsRange(search): %v", err)
	}
	if len(domsSearch) != 1 || domsSearch[0].Domain != "youtube.com" {
		t.Fatalf("unexpected domain search result: %+v", domsSearch)
	}

	// Users in 7-day window (charlie from 10 days ago must be excluded)
	usersWeek, err := db.GetUsersRange(weekStart, today)
	if err != nil {
		t.Fatalf("GetUsersRange: %v", err)
	}
	if len(usersWeek) != 2 || usersWeek[0].Username != "alice" || usersWeek[1].Username != "bob" {
		t.Fatalf("unexpected 7d users: %+v", usersWeek)
	}

	// Countries in 7-day window
	countriesWeek, err := db.GetCountryStatsRange(weekStart, today, "")
	if err != nil {
		t.Fatalf("GetCountryStatsRange: %v", err)
	}
	if len(countriesWeek) != 2 || countriesWeek[0].Country != "JP" || countriesWeek[1].Country != "US" {
		t.Fatalf("unexpected 7d countries: %+v", countriesWeek)
	}

	// Daily trends over 7 days must be zero-filled to exactly 7 points
	dailyTrends, err := db.GetTrendsRange(weekStart, today, "day", "")
	if err != nil {
		t.Fatalf("GetTrendsRange(day): %v", err)
	}
	if len(dailyTrends) != 7 {
		t.Fatalf("len(dailyTrends) = %d, want 7", len(dailyTrends))
	}
	if dailyTrends[6].Date != today || dailyTrends[6].Download != 5500 {
		t.Fatalf("unexpected last day trend point: %+v", dailyTrends[6])
	}

	// Weekly rollup over 14 days produces 2 buckets
	start14 := now.AddDate(0, 0, -13).Format("2006-01-02")
	weeklyTrends, err := db.GetTrendsRange(start14, today, "week", "")
	if err != nil {
		t.Fatalf("GetTrendsRange(week): %v", err)
	}
	if len(weeklyTrends) != 2 {
		t.Fatalf("len(weeklyTrends) = %d, want 2", len(weeklyTrends))
	}
	if weeklyTrends[0].Download != 700 { // charlie 10 days ago
		t.Fatalf("unexpected week 0 download: %+v", weeklyTrends[0])
	}
	if weeklyTrends[1].Download != 13500 { // alice+bob yesterday+today
		t.Fatalf("unexpected week 1 download: %+v", weeklyTrends[1])
	}

	// Hourly breakdown for yesterday must have 24 zero-filled points
	hourlyYesterday, err := db.GetTrendsRange(yesterday, yesterday, "hour", "")
	if err != nil {
		t.Fatalf("GetTrendsRange(hour): %v", err)
	}
	if len(hourlyYesterday) != 24 {
		t.Fatalf("len(hourlyYesterday) = %d, want 24", len(hourlyYesterday))
	}
	if hourlyYesterday[14].Download != 8000 {
		t.Fatalf("unexpected hour 14 point: %+v", hourlyYesterday[14])
	}
}

func TestStatsDB_RetentionCleanup90Days(t *testing.T) {
	dir := t.TempDir()
	db, err := NewStatsDB(filepath.Join(dir, "cleanup.db"))
	if err != nil {
		t.Fatalf("NewStatsDB: %v", err)
	}
	defer db.Close()

	now := time.Now()
	oldTime := now.AddDate(0, 0, -95)
	keepTime := now.AddDate(0, 0, -10)

	records := []TrafficRecord{
		{
			Username: "alice", Domain: "expired.com", Country: "US", CountryName: "United States", Continent: "NA",
			Upload: 100, Download: 200, ConnCount: 1,
			Date: oldTime.Format("2006-01-02"), Hour: oldTime.Format("2006-01-02T15:00:00"),
			Minute: oldTime.Format("2006-01-02T15:04:00"), Timestamp: oldTime,
		},
		{
			Username: "alice", Domain: "recent.com", Country: "JP", CountryName: "Japan", Continent: "AS",
			Upload: 300, Download: 600, ConnCount: 1,
			Date: keepTime.Format("2006-01-02"), Hour: keepTime.Format("2006-01-02T15:00:00"),
			Minute: keepTime.Format("2006-01-02T15:04:00"), Timestamp: keepTime,
		},
	}
	if err := db.BatchUpsert(records); err != nil {
		t.Fatalf("BatchUpsert: %v", err)
	}

	db.CleanupOldData(7, 90, 90)

	ov, err := db.GetOverviewRange(oldTime.Format("2006-01-02"), now.Format("2006-01-02"), "")
	if err != nil {
		t.Fatalf("GetOverviewRange after cleanup: %v", err)
	}
	if ov.TotalUpload != 300 || ov.TotalDownload != 600 || ov.DomainCount != 1 || ov.CountryCount != 1 {
		t.Fatalf("expected only recent record to survive cleanup, got %+v", ov)
	}
}

func TestStatsDB_DisabledCacheReloadsOnOpen(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "cache.db")

	db1, err := NewStatsDB(dbPath)
	if err != nil {
		t.Fatalf("NewStatsDB(1): %v", err)
	}
	if err := db1.SetUserDisabled("blocked_user", true); err != nil {
		t.Fatalf("SetUserDisabled: %v", err)
	}
	db1.Close()

	db2, err := NewStatsDB(dbPath)
	if err != nil {
		t.Fatalf("NewStatsDB(2): %v", err)
	}
	defer db2.Close()

	if !db2.IsUserDisabled("blocked_user") {
		t.Fatal("expected blocked_user to be disabled from reloaded in-memory cache")
	}
	if db2.IsUserDisabled("active_user") {
		t.Fatal("expected active_user to not be disabled")
	}
}

func TestV2API_PeriodAndCompareEndpoints(t *testing.T) {
	dir := t.TempDir()
	db, err := NewStatsDB(filepath.Join(dir, "api.db"))
	if err != nil {
		t.Fatalf("NewStatsDB: %v", err)
	}
	defer db.Close()

	now := time.Now()
	today := now.Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")

	if err := db.BatchUpsert([]TrafficRecord{
		{
			Username: "alice", Domain: "google.com", Country: "US", CountryName: "United States", Continent: "NA",
			Upload: 1000, Download: 3000, ConnCount: 2, Date: today, Hour: today + "T12:00:00", Timestamp: now,
		},
		{
			Username: "alice", Domain: "google.com", Country: "US", CountryName: "United States", Continent: "NA",
			Upload: 500, Download: 1500, ConnCount: 1, Date: yesterday, Hour: yesterday + "T12:00:00", Timestamp: now.AddDate(0, 0, -1),
		},
	}); err != nil {
		t.Fatalf("BatchUpsert: %v", err)
	}

	mux := http.NewServeMux()
	registerV2API(mux, db, 90)

	// 1. /api/v2/meta
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v2/meta", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/v2/meta status = %d", rec.Code)
	}
	var metaResp struct {
		Success bool `json:"success"`
		Data    struct {
			RetentionDays int    `json:"retention_days"`
			Today         string `json:"today"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &metaResp); err != nil {
		t.Fatalf("unmarshal meta: %v", err)
	}
	if !metaResp.Success || metaResp.Data.RetentionDays != 90 || metaResp.Data.Today != today {
		t.Fatalf("unexpected meta response: %+v", metaResp)
	}

	// 2. /api/v2/overview with compare=1
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v2/overview?start="+today+"&end="+today+"&compare=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/v2/overview status = %d", rec.Code)
	}
	var ovResp struct {
		Success bool          `json:"success"`
		Data    OverviewStats `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &ovResp); err != nil {
		t.Fatalf("unmarshal overview: %v", err)
	}
	if ovResp.Data.TotalDownload != 3000 || ovResp.Data.Previous == nil || ovResp.Data.Previous.TotalDownload != 1500 {
		t.Fatalf("unexpected overview compare response: %+v", ovResp.Data)
	}

	// 3. /api/v2/trends?start=...&end=...&granularity=day
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v2/trends?start="+yesterday+"&end="+today+"&granularity=day", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/v2/trends status = %d", rec.Code)
	}
	var trendResp struct {
		Success bool           `json:"success"`
		Data    []DBTrendPoint `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &trendResp); err != nil {
		t.Fatalf("unmarshal trends: %v", err)
	}
	if len(trendResp.Data) != 2 || trendResp.Data[0].Download != 1500 || trendResp.Data[1].Download != 3000 {
		t.Fatalf("unexpected trends response: %+v", trendResp.Data)
	}
}

func TestStatsDB_ReadWriteSeparationAndTemplateParse(t *testing.T) {
	dir := t.TempDir()
	db, err := NewStatsDB(filepath.Join(dir, "rw.db"))
	if err != nil {
		t.Fatalf("NewStatsDB: %v", err)
	}
	defer db.Close()

	if db.readDB == nil || db.readDB == db.db {
		t.Fatal("expected separate read-only SQLite pool in WAL mode")
	}

	// Verify readDB rejects writes (query_only=ON)
	if _, err := db.readDB.Exec(`INSERT INTO user_stats (username) VALUES ('should_fail')`); err == nil {
		t.Fatal("expected readDB to reject write query")
	}

	// Verify embedded dashboard_v2.html parses cleanly with html/template
	tmpl, err := template.New("").ParseFS(templatesFS, "templates/dashboard_v2.html")
	if err != nil {
		t.Fatalf("parse dashboard_v2.html failed: %v", err)
	}
	rec := httptest.NewRecorder()
	if err := tmpl.ExecuteTemplate(rec, "dashboard_v2.html", nil); err != nil {
		t.Fatalf("execute dashboard_v2.html failed: %v", err)
	}
	if rec.Body.Len() == 0 {
		t.Fatal("rendered dashboard_v2.html is empty")
	}
}
