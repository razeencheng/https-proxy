package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// StatsDB wraps the SQLite database for traffic statistics storage.
// It uses WAL mode with separate writer and reader pools plus an in-memory
// disabled-user cache so proxy forwarding never blocks on SQLite I/O.
type StatsDB struct {
	db     *sql.DB // single-connection writer pool
	readDB *sql.DB // multi-connection read-only pool (shares WAL snapshot)

	disabledMu    sync.RWMutex
	disabledUsers map[string]bool
}

// NewStatsDB opens (or creates) a SQLite database at dbPath and initialises
// all required tables, indexes, and in-memory caches.
func NewStatsDB(dbPath string) (*StatsDB, error) {
	if dbPath != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
	}

	writeDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// Single writer connection keeps WAL writes serialized without SQLITE_BUSY.
	writeDB.SetMaxOpenConns(1)

	// Enable incremental auto_vacuum for freshly created databases (no-op if already initialized).
	_, _ = writeDB.Exec("PRAGMA auto_vacuum=INCREMENTAL")

	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA cache_size=-16000", // 16 MB page cache
		"PRAGMA busy_timeout=5000",
		"PRAGMA temp_store=MEMORY",
	} {
		if _, err := writeDB.Exec(pragma); err != nil {
			writeDB.Close()
			return nil, fmt.Errorf("exec %s: %w", pragma, err)
		}
	}

	sdb := &StatsDB{
		db:            writeDB,
		readDB:        writeDB,
		disabledUsers: make(map[string]bool),
	}
	if err := sdb.initTables(); err != nil {
		writeDB.Close()
		return nil, err
	}

	// Open a separate read-only pool so dashboard queries never contend with background flushes.
	if dbPath != ":memory:" {
		absPath, err := filepath.Abs(dbPath)
		if err == nil {
			u := url.URL{
				Scheme:   "file",
				Path:     filepath.ToSlash(absPath),
				RawQuery: "_pragma=busy_timeout(5000)&_pragma=query_only(ON)&_pragma=cache_size(-8000)&_pragma=temp_store(MEMORY)",
			}
			if rdb, err := sql.Open("sqlite", u.String()); err == nil {
				rdb.SetMaxOpenConns(4)
				rdb.SetMaxIdleConns(2)
				if pingErr := rdb.Ping(); pingErr == nil {
					sdb.readDB = rdb
				} else {
					rdb.Close()
				}
			}
		}
	}

	sdb.backfillDailyFromHourly()
	if err := sdb.loadDisabledUsersCache(); err != nil {
		log.Printf("[DB] Warning: failed to load disabled users cache: %v", err)
	}

	return sdb, nil
}

func (s *StatsDB) initTables() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS user_stats (
			username       TEXT PRIMARY KEY,
			total_upload   INTEGER DEFAULT 0,
			total_download INTEGER DEFAULT 0,
			conn_count     INTEGER DEFAULT 0,
			request_count  INTEGER DEFAULT 0,
			first_seen     DATETIME,
			last_access    DATETIME,
			disabled       INTEGER DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS domain_stats (
			user       TEXT NOT NULL,
			domain     TEXT NOT NULL,
			upload     INTEGER DEFAULT 0,
			download   INTEGER DEFAULT 0,
			conn_count INTEGER DEFAULT 0,
			last_seen  DATETIME,
			PRIMARY KEY (user, domain)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_domain_total_traffic ON domain_stats(upload + download)`,
		`CREATE TABLE IF NOT EXISTS minute_stats (
			user       TEXT NOT NULL,
			minute     TEXT NOT NULL,
			upload     INTEGER DEFAULT 0,
			download   INTEGER DEFAULT 0,
			conn_count INTEGER DEFAULT 0,
			PRIMARY KEY (user, minute)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_minute_stats_time ON minute_stats(minute)`,
		`CREATE TABLE IF NOT EXISTS hourly_stats (
			user       TEXT NOT NULL,
			hour       TEXT NOT NULL,
			upload     INTEGER DEFAULT 0,
			download   INTEGER DEFAULT 0,
			conn_count INTEGER DEFAULT 0,
			PRIMARY KEY (user, hour)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_hourly_stats_time ON hourly_stats(hour)`,
		`CREATE TABLE IF NOT EXISTS country_stats (
			user         TEXT NOT NULL,
			country      TEXT NOT NULL,
			country_name TEXT,
			continent    TEXT,
			upload       INTEGER DEFAULT 0,
			download     INTEGER DEFAULT 0,
			conn_count   INTEGER DEFAULT 0,
			last_seen    DATETIME,
			PRIMARY KEY (user, country)
		)`,
		`CREATE TABLE IF NOT EXISTS daily_user_stats (
			date          TEXT NOT NULL,
			user          TEXT NOT NULL,
			upload        INTEGER DEFAULT 0,
			download      INTEGER DEFAULT 0,
			conn_count    INTEGER DEFAULT 0,
			request_count INTEGER DEFAULT 0,
			PRIMARY KEY (date, user)
		)`,
		`CREATE TABLE IF NOT EXISTS daily_domain_stats (
			date       TEXT NOT NULL,
			user       TEXT NOT NULL,
			domain     TEXT NOT NULL,
			upload     INTEGER DEFAULT 0,
			download   INTEGER DEFAULT 0,
			conn_count INTEGER DEFAULT 0,
			PRIMARY KEY (date, user, domain)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_daily_domain_date_domain ON daily_domain_stats(date, domain)`,
		`CREATE TABLE IF NOT EXISTS daily_country_stats (
			date         TEXT NOT NULL,
			user         TEXT NOT NULL,
			country      TEXT NOT NULL,
			country_name TEXT,
			continent    TEXT,
			upload       INTEGER DEFAULT 0,
			download     INTEGER DEFAULT 0,
			conn_count   INTEGER DEFAULT 0,
			PRIMARY KEY (date, user, country)
		)`,
		`CREATE TABLE IF NOT EXISTS retention_config (
			key   TEXT PRIMARY KEY,
			value TEXT
		)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("init table: %w\nSQL: %s", err, stmt)
		}
	}
	return nil
}

// backfillDailyFromHourly performs a one-time backfill of daily_user_stats from
// existing hourly_stats when upgrading an existing database.
func (s *StatsDB) backfillDailyFromHourly() {
	var dailyCount int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM daily_user_stats`).Scan(&dailyCount); err != nil || dailyCount > 0 {
		return
	}
	res, err := s.db.Exec(`INSERT OR IGNORE INTO daily_user_stats (date, user, upload, download, conn_count, request_count)
		SELECT substr(hour, 1, 10), user, SUM(upload), SUM(download), SUM(conn_count), 0
		FROM hourly_stats
		WHERE length(hour) >= 10
		GROUP BY substr(hour, 1, 10), user`)
	if err != nil {
		log.Printf("[DB] Backfill daily_user_stats failed: %v", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("[DB] Backfilled %d daily_user_stats rows from hourly_stats", n)
	}
}

func (s *StatsDB) loadDisabledUsersCache() error {
	rows, err := s.db.Query(`SELECT username FROM user_stats WHERE disabled != 0`)
	if err != nil {
		return err
	}
	defer rows.Close()

	cache := make(map[string]bool)
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return err
		}
		cache[u] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}

	s.disabledMu.Lock()
	s.disabledUsers = cache
	s.disabledMu.Unlock()
	return nil
}

// Close closes the database connections.
func (s *StatsDB) Close() error {
	var readErr error
	if s.readDB != nil && s.readDB != s.db {
		readErr = s.readDB.Close()
	}
	writeErr := s.db.Close()
	if writeErr != nil {
		return writeErr
	}
	return readErr
}

// ---------------------------------------------------------------------------
// Batch upsert helpers – called from StatsCollector.flush()
// ---------------------------------------------------------------------------

// TrafficRecord holds a single aggregated traffic record ready for DB upsert.
type TrafficRecord struct {
	Username     string
	Domain       string
	Upload       uint64
	Download     uint64
	ConnCount    int
	RequestCount int
	Country      string
	CountryName  string
	Continent    string
	Minute       string // "2006-01-02T15:04:00"
	Hour         string // "2006-01-02T15:00:00"
	Date         string // "2006-01-02"
	Timestamp    time.Time
}

func resolveRecordDate(r TrafficRecord) string {
	if r.Date != "" {
		return r.Date
	}
	if len(r.Hour) >= 10 {
		return r.Hour[:10]
	}
	if len(r.Minute) >= 10 {
		return r.Minute[:10]
	}
	if !r.Timestamp.IsZero() {
		return r.Timestamp.Format("2006-01-02")
	}
	return ""
}

// BatchUpsert writes a slice of TrafficRecords into all stat tables inside a
// single transaction, pre-aggregating by key to minimize SQLite write statements.
func (s *StatsDB) BatchUpsert(records []TrafficRecord) error {
	if len(records) == 0 {
		return nil
	}

	type userSum struct {
		upload, download uint64
		conns, reqs      int
		lastTS           time.Time
	}
	type timeBucketKey struct{ user, bucket string }
	type timeBucketSum struct {
		upload, download uint64
		conns, reqs      int
	}
	type domainKey struct{ user, domain string }
	type domainSum struct {
		upload, download uint64
		conns            int
		lastTS           time.Time
	}
	type dailyDomainKey struct{ date, user, domain string }
	type countryKey struct{ user, country string }
	type countrySum struct {
		countryName, continent string
		upload, download       uint64
		conns                  int
		lastTS                 time.Time
	}
	type dailyCountryKey struct{ date, user, country string }

	users := make(map[string]*userSum)
	domains := make(map[domainKey]*domainSum)
	countries := make(map[countryKey]*countrySum)
	minutes := make(map[timeBucketKey]*timeBucketSum)
	hours := make(map[timeBucketKey]*timeBucketSum)
	dailyUsers := make(map[timeBucketKey]*timeBucketSum)
	dailyDomains := make(map[dailyDomainKey]*domainSum)
	dailyCountries := make(map[dailyCountryKey]*countrySum)

	for _, r := range records {
		if r.Username == "" {
			continue
		}
		ts := r.Timestamp
		if ts.IsZero() {
			ts = time.Now()
		}
		date := resolveRecordDate(r)

		u := users[r.Username]
		if u == nil {
			u = &userSum{}
			users[r.Username] = u
		}
		u.upload += r.Upload
		u.download += r.Download
		u.conns += r.ConnCount
		u.reqs += r.RequestCount
		if ts.After(u.lastTS) {
			u.lastTS = ts
		}

		if r.Domain != "" {
			dk := domainKey{r.Username, r.Domain}
			d := domains[dk]
			if d == nil {
				d = &domainSum{}
				domains[dk] = d
			}
			d.upload += r.Upload
			d.download += r.Download
			d.conns += r.ConnCount
			if ts.After(d.lastTS) {
				d.lastTS = ts
			}

			if date != "" {
				ddk := dailyDomainKey{date, r.Username, r.Domain}
				dd := dailyDomains[ddk]
				if dd == nil {
					dd = &domainSum{}
					dailyDomains[ddk] = dd
				}
				dd.upload += r.Upload
				dd.download += r.Download
				dd.conns += r.ConnCount
			}
		}

		if r.Minute != "" && (r.Upload > 0 || r.Download > 0 || r.ConnCount > 0) {
			mk := timeBucketKey{r.Username, r.Minute}
			m := minutes[mk]
			if m == nil {
				m = &timeBucketSum{}
				minutes[mk] = m
			}
			m.upload += r.Upload
			m.download += r.Download
			m.conns += r.ConnCount
		}

		if r.Hour != "" && (r.Upload > 0 || r.Download > 0 || r.ConnCount > 0) {
			hk := timeBucketKey{r.Username, r.Hour}
			h := hours[hk]
			if h == nil {
				h = &timeBucketSum{}
				hours[hk] = h
			}
			h.upload += r.Upload
			h.download += r.Download
			h.conns += r.ConnCount
		}

		if date != "" {
			duk := timeBucketKey{r.Username, date}
			du := dailyUsers[duk]
			if du == nil {
				du = &timeBucketSum{}
				dailyUsers[duk] = du
			}
			du.upload += r.Upload
			du.download += r.Download
			du.conns += r.ConnCount
			du.reqs += r.RequestCount
		}

		if r.Country != "" {
			ck := countryKey{r.Username, r.Country}
			c := countries[ck]
			if c == nil {
				c = &countrySum{countryName: r.CountryName, continent: r.Continent}
				countries[ck] = c
			}
			if c.countryName == "" {
				c.countryName = r.CountryName
			}
			if c.continent == "" {
				c.continent = r.Continent
			}
			c.upload += r.Upload
			c.download += r.Download
			c.conns += r.ConnCount
			if ts.After(c.lastTS) {
				c.lastTS = ts
			}

			if date != "" {
				dck := dailyCountryKey{date, r.Username, r.Country}
				dc := dailyCountries[dck]
				if dc == nil {
					dc = &countrySum{countryName: r.CountryName, continent: r.Continent}
					dailyCountries[dck] = dc
				}
				if dc.countryName == "" {
					dc.countryName = r.CountryName
				}
				if dc.continent == "" {
					dc.continent = r.Continent
				}
				dc.upload += r.Upload
				dc.download += r.Download
				dc.conns += r.ConnCount
			}
		}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	stmtUser, err := tx.Prepare(`INSERT INTO user_stats (username, total_upload, total_download, conn_count, request_count, first_seen, last_access)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(username) DO UPDATE SET
			total_upload   = total_upload   + excluded.total_upload,
			total_download = total_download + excluded.total_download,
			conn_count     = conn_count     + excluded.conn_count,
			request_count  = request_count  + excluded.request_count,
			last_access    = excluded.last_access`)
	if err != nil {
		return fmt.Errorf("prepare user_stats: %w", err)
	}
	defer stmtUser.Close()

	for username, u := range users {
		ts := u.lastTS.Format(time.RFC3339)
		if _, err := stmtUser.Exec(username, u.upload, u.download, u.conns, u.reqs, ts, ts); err != nil {
			return fmt.Errorf("exec user_stats (%s): %w", username, err)
		}
	}

	if len(domains) > 0 {
		stmtDomain, err := tx.Prepare(`INSERT INTO domain_stats (user, domain, upload, download, conn_count, last_seen)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(user, domain) DO UPDATE SET
				upload     = upload     + excluded.upload,
				download   = download   + excluded.download,
				conn_count = conn_count + excluded.conn_count,
				last_seen  = excluded.last_seen`)
		if err != nil {
			return fmt.Errorf("prepare domain_stats: %w", err)
		}
		defer stmtDomain.Close()

		for k, d := range domains {
			if _, err := stmtDomain.Exec(k.user, k.domain, d.upload, d.download, d.conns, d.lastTS.Format(time.RFC3339)); err != nil {
				return fmt.Errorf("exec domain_stats (%s/%s): %w", k.user, k.domain, err)
			}
		}
	}

	if len(minutes) > 0 {
		stmtMinute, err := tx.Prepare(`INSERT INTO minute_stats (user, minute, upload, download, conn_count)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(user, minute) DO UPDATE SET
				upload     = upload     + excluded.upload,
				download   = download   + excluded.download,
				conn_count = conn_count + excluded.conn_count`)
		if err != nil {
			return fmt.Errorf("prepare minute_stats: %w", err)
		}
		defer stmtMinute.Close()

		for k, m := range minutes {
			if _, err := stmtMinute.Exec(k.user, k.bucket, m.upload, m.download, m.conns); err != nil {
				return fmt.Errorf("exec minute_stats: %w", err)
			}
		}
	}

	if len(hours) > 0 {
		stmtHour, err := tx.Prepare(`INSERT INTO hourly_stats (user, hour, upload, download, conn_count)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(user, hour) DO UPDATE SET
				upload     = upload     + excluded.upload,
				download   = download   + excluded.download,
				conn_count = conn_count + excluded.conn_count`)
		if err != nil {
			return fmt.Errorf("prepare hourly_stats: %w", err)
		}
		defer stmtHour.Close()

		for k, h := range hours {
			if _, err := stmtHour.Exec(k.user, k.bucket, h.upload, h.download, h.conns); err != nil {
				return fmt.Errorf("exec hourly_stats: %w", err)
			}
		}
	}

	if len(countries) > 0 {
		stmtCountry, err := tx.Prepare(`INSERT INTO country_stats (user, country, country_name, continent, upload, download, conn_count, last_seen)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(user, country) DO UPDATE SET
				country_name = COALESCE(NULLIF(excluded.country_name, ''), country_name),
				continent    = COALESCE(NULLIF(excluded.continent, ''), continent),
				upload       = upload     + excluded.upload,
				download     = download   + excluded.download,
				conn_count   = conn_count + excluded.conn_count,
				last_seen    = excluded.last_seen`)
		if err != nil {
			return fmt.Errorf("prepare country_stats: %w", err)
		}
		defer stmtCountry.Close()

		for k, c := range countries {
			if _, err := stmtCountry.Exec(k.user, k.country, c.countryName, c.continent, c.upload, c.download, c.conns, c.lastTS.Format(time.RFC3339)); err != nil {
				return fmt.Errorf("exec country_stats: %w", err)
			}
		}
	}

	if len(dailyUsers) > 0 {
		stmtDailyUser, err := tx.Prepare(`INSERT INTO daily_user_stats (date, user, upload, download, conn_count, request_count)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(date, user) DO UPDATE SET
				upload        = upload        + excluded.upload,
				download      = download      + excluded.download,
				conn_count    = conn_count    + excluded.conn_count,
				request_count = request_count + excluded.request_count`)
		if err != nil {
			return fmt.Errorf("prepare daily_user_stats: %w", err)
		}
		defer stmtDailyUser.Close()

		for k, du := range dailyUsers {
			if _, err := stmtDailyUser.Exec(k.bucket, k.user, du.upload, du.download, du.conns, du.reqs); err != nil {
				return fmt.Errorf("exec daily_user_stats: %w", err)
			}
		}
	}

	if len(dailyDomains) > 0 {
		stmtDailyDomain, err := tx.Prepare(`INSERT INTO daily_domain_stats (date, user, domain, upload, download, conn_count)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(date, user, domain) DO UPDATE SET
				upload     = upload     + excluded.upload,
				download   = download   + excluded.download,
				conn_count = conn_count + excluded.conn_count`)
		if err != nil {
			return fmt.Errorf("prepare daily_domain_stats: %w", err)
		}
		defer stmtDailyDomain.Close()

		for k, dd := range dailyDomains {
			if _, err := stmtDailyDomain.Exec(k.date, k.user, k.domain, dd.upload, dd.download, dd.conns); err != nil {
				return fmt.Errorf("exec daily_domain_stats: %w", err)
			}
		}
	}

	if len(dailyCountries) > 0 {
		stmtDailyCountry, err := tx.Prepare(`INSERT INTO daily_country_stats (date, user, country, country_name, continent, upload, download, conn_count)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(date, user, country) DO UPDATE SET
				country_name = COALESCE(NULLIF(excluded.country_name, ''), country_name),
				continent    = COALESCE(NULLIF(excluded.continent, ''), continent),
				upload       = upload     + excluded.upload,
				download     = download   + excluded.download,
				conn_count   = conn_count + excluded.conn_count`)
		if err != nil {
			return fmt.Errorf("prepare daily_country_stats: %w", err)
		}
		defer stmtDailyCountry.Close()

		for k, dc := range dailyCountries {
			if _, err := stmtDailyCountry.Exec(k.date, k.user, k.country, dc.countryName, dc.continent, dc.upload, dc.download, dc.conns); err != nil {
				return fmt.Errorf("exec daily_country_stats: %w", err)
			}
		}
	}

	return tx.Commit()
}

// ---------------------------------------------------------------------------
// Query helpers – used by API layer (uses readDB pool)
// ---------------------------------------------------------------------------

// OverviewStats holds global or period-scoped overview numbers.
type OverviewStats struct {
	TotalUpload   uint64         `json:"total_upload"`
	TotalDownload uint64         `json:"total_download"`
	TotalConns    uint64         `json:"total_connections"`
	DomainCount   int            `json:"domain_count"`
	UserCount     int            `json:"user_count"`
	CountryCount  int            `json:"country_count"`
	Previous      *OverviewStats `json:"previous,omitempty"`
}

func (s *StatsDB) GetOverview() (*OverviewStats, error) {
	o := &OverviewStats{}
	err := s.readDB.QueryRow(`SELECT COALESCE(SUM(total_upload),0), COALESCE(SUM(total_download),0), COALESCE(SUM(conn_count),0), COUNT(*) FROM user_stats`).
		Scan(&o.TotalUpload, &o.TotalDownload, &o.TotalConns, &o.UserCount)
	if err != nil {
		return nil, err
	}
	s.readDB.QueryRow(`SELECT COUNT(DISTINCT domain) FROM domain_stats`).Scan(&o.DomainCount)
	s.readDB.QueryRow(`SELECT COUNT(DISTINCT country) FROM country_stats`).Scan(&o.CountryCount)
	return o, nil
}

// GetOverviewRange returns overview metrics aggregated over [startDate, endDate] (inclusive).
// Optional user filters to a single user.
func (s *StatsDB) GetOverviewRange(startDate, endDate, user string) (*OverviewStats, error) {
	if startDate == "" || endDate == "" {
		return s.GetOverview()
	}

	o := &OverviewStats{}
	args := []interface{}{startDate, endDate}
	userClause := ""
	if user != "" {
		userClause = " AND user = ?"
		args = append(args, user)
	}

	err := s.readDB.QueryRow(
		`SELECT COALESCE(SUM(upload),0), COALESCE(SUM(download),0), COALESCE(SUM(conn_count),0), COUNT(DISTINCT user)
		 FROM daily_user_stats WHERE date >= ? AND date <= ?`+userClause,
		args...,
	).Scan(&o.TotalUpload, &o.TotalDownload, &o.TotalConns, &o.UserCount)
	if err != nil {
		return nil, err
	}

	if err := s.readDB.QueryRow(
		`SELECT COUNT(DISTINCT domain) FROM daily_domain_stats WHERE date >= ? AND date <= ?`+userClause,
		args...,
	).Scan(&o.DomainCount); err != nil {
		return nil, err
	}

	if err := s.readDB.QueryRow(
		`SELECT COUNT(DISTINCT country) FROM daily_country_stats WHERE date >= ? AND date <= ?`+userClause,
		args...,
	).Scan(&o.CountryCount); err != nil {
		return nil, err
	}

	return o, nil
}

// DBUserStats holds a single user's stats from the database.
type DBUserStats struct {
	Username      string `json:"username"`
	TotalUpload   uint64 `json:"total_upload"`
	TotalDownload uint64 `json:"total_download"`
	ConnCount     uint64 `json:"conn_count"`
	RequestCount  uint64 `json:"request_count"`
	FirstSeen     string `json:"first_seen"`
	LastAccess    string `json:"last_access"`
	Disabled      bool   `json:"disabled"`
}

func (s *StatsDB) GetAllUsers() ([]DBUserStats, error) {
	rows, err := s.readDB.Query(`SELECT username, total_upload, total_download, conn_count, request_count, COALESCE(first_seen,''), COALESCE(last_access,''), disabled FROM user_stats ORDER BY total_upload+total_download DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []DBUserStats
	for rows.Next() {
		var u DBUserStats
		var dis int
		if err := rows.Scan(&u.Username, &u.TotalUpload, &u.TotalDownload, &u.ConnCount, &u.RequestCount, &u.FirstSeen, &u.LastAccess, &dis); err != nil {
			return nil, err
		}
		u.Disabled = dis != 0
		users = append(users, u)
	}
	return users, rows.Err()
}

// GetUsersRange returns per-user traffic aggregated over [startDate, endDate] (inclusive).
func (s *StatsDB) GetUsersRange(startDate, endDate string) ([]DBUserStats, error) {
	if startDate == "" || endDate == "" {
		return s.GetAllUsers()
	}

	rows, err := s.readDB.Query(`
		SELECT d.user,
		       COALESCE(SUM(d.upload), 0),
		       COALESCE(SUM(d.download), 0),
		       COALESCE(SUM(d.conn_count), 0),
		       COALESCE(SUM(d.request_count), 0),
		       COALESCE(u.first_seen, ''),
		       COALESCE(u.last_access, ''),
		       COALESCE(u.disabled, 0)
		FROM daily_user_stats d
		LEFT JOIN user_stats u ON u.username = d.user
		WHERE d.date >= ? AND d.date <= ?
		GROUP BY d.user
		ORDER BY SUM(d.upload) + SUM(d.download) DESC`,
		startDate, endDate,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []DBUserStats
	for rows.Next() {
		var u DBUserStats
		var dis int
		if err := rows.Scan(&u.Username, &u.TotalUpload, &u.TotalDownload, &u.ConnCount, &u.RequestCount, &u.FirstSeen, &u.LastAccess, &dis); err != nil {
			return nil, err
		}
		u.Disabled = dis != 0
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *StatsDB) GetUser(username string) (*DBUserStats, error) {
	u := &DBUserStats{}
	var dis int
	err := s.readDB.QueryRow(`SELECT username, total_upload, total_download, conn_count, request_count, COALESCE(first_seen,''), COALESCE(last_access,''), disabled FROM user_stats WHERE username=?`, username).
		Scan(&u.Username, &u.TotalUpload, &u.TotalDownload, &u.ConnCount, &u.RequestCount, &u.FirstSeen, &u.LastAccess, &dis)
	if err != nil {
		return nil, err
	}
	u.Disabled = dis != 0
	return u, nil
}

// DBDomainStats holds domain-level stats.
type DBDomainStats struct {
	User      string `json:"user,omitempty"`
	Domain    string `json:"domain"`
	Upload    uint64 `json:"upload"`
	Download  uint64 `json:"download"`
	ConnCount uint64 `json:"conn_count"`
	LastSeen  string `json:"last_seen,omitempty"`
}

func (s *StatsDB) GetTopDomains(limit int, user string) ([]DBDomainStats, error) {
	q := `SELECT user, domain, upload, download, conn_count, COALESCE(last_seen,'') FROM domain_stats`
	var args []interface{}
	if user != "" {
		q += ` WHERE user=?`
		args = append(args, user)
	}
	q += ` ORDER BY upload+download DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.readDB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DBDomainStats
	for rows.Next() {
		var d DBDomainStats
		if err := rows.Scan(&d.User, &d.Domain, &d.Upload, &d.Download, &d.ConnCount, &d.LastSeen); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// GetTopDomainsRange returns top domains aggregated over [startDate, endDate] (inclusive).
func (s *StatsDB) GetTopDomainsRange(startDate, endDate string, limit int, user, search string) ([]DBDomainStats, error) {
	if startDate == "" || endDate == "" {
		return s.GetTopDomains(limit, user)
	}
	if limit <= 0 {
		limit = 50
	}

	q := `SELECT domain, COALESCE(SUM(upload),0), COALESCE(SUM(download),0), COALESCE(SUM(conn_count),0)
	      FROM daily_domain_stats WHERE date >= ? AND date <= ?`
	args := []interface{}{startDate, endDate}

	if user != "" {
		q += ` AND user = ?`
		args = append(args, user)
	}
	if search = strings.TrimSpace(search); search != "" {
		q += ` AND LOWER(domain) LIKE ?`
		args = append(args, "%"+strings.ToLower(search)+"%")
	}
	q += ` GROUP BY domain ORDER BY SUM(upload) + SUM(download) DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.readDB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DBDomainStats
	for rows.Next() {
		var d DBDomainStats
		if err := rows.Scan(&d.Domain, &d.Upload, &d.Download, &d.ConnCount); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DBTrendPoint holds a single time-series data point.
type DBTrendPoint struct {
	Time     string `json:"time"`
	Date     string `json:"date,omitempty"`
	Upload   uint64 `json:"upload"`
	Download uint64 `json:"download"`
	Conns    uint64 `json:"connections"`
}

// GetTrends fetches time-series data. rangeStr is one of "30m","1h","24h","7d".
func (s *StatsDB) GetTrends(rangeStr string) ([]DBTrendPoint, error) {
	var table, since string
	now := time.Now()
	switch rangeStr {
	case "30m":
		table = "minute_stats"
		since = now.Add(-30 * time.Minute).Format("2006-01-02T15:04:00")
	case "1h":
		table = "minute_stats"
		since = now.Add(-1 * time.Hour).Format("2006-01-02T15:04:00")
	case "24h":
		table = "hourly_stats"
		since = now.Add(-24 * time.Hour).Format("2006-01-02T15:00:00")
	case "7d":
		table = "hourly_stats"
		since = now.Add(-7 * 24 * time.Hour).Format("2006-01-02T15:00:00")
	default:
		table = "minute_stats"
		since = now.Add(-1 * time.Hour).Format("2006-01-02T15:04:00")
	}

	timeCol := "minute"
	if table == "hourly_stats" {
		timeCol = "hour"
	}

	q := fmt.Sprintf(`SELECT %s, SUM(upload), SUM(download), SUM(conn_count) FROM %s WHERE %s>=? GROUP BY %s ORDER BY %s`, timeCol, table, timeCol, timeCol, timeCol)
	rows, err := s.readDB.Query(q, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DBTrendPoint
	for rows.Next() {
		var p DBTrendPoint
		if err := rows.Scan(&p.Time, &p.Upload, &p.Download, &p.Conns); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetTrendsRange fetches time-series data for [startDate, endDate] with specified
// granularity ("30m", "1h", "hour", "day", "week") and optional user filter.
func (s *StatsDB) GetTrendsRange(startDate, endDate, granularity, user string) ([]DBTrendPoint, error) {
	now := time.Now()
	todayStr := now.Format("2006-01-02")

	// Live minute-level windows (30m / 1h)
	if granularity == "30m" || granularity == "1h" {
		mins := 60
		if granularity == "30m" {
			mins = 30
		}
		startTime := now.Truncate(time.Minute).Add(-time.Duration(mins-1) * time.Minute)
		since := startTime.Format("2006-01-02T15:04:00")

		q := `SELECT minute, SUM(upload), SUM(download), SUM(conn_count) FROM minute_stats WHERE minute >= ?`
		args := []interface{}{since}
		if user != "" {
			q += ` AND user = ?`
			args = append(args, user)
		}
		q += ` GROUP BY minute ORDER BY minute`

		rows, err := s.readDB.Query(q, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		byMin := make(map[string]DBTrendPoint)
		for rows.Next() {
			var p DBTrendPoint
			if err := rows.Scan(&p.Time, &p.Upload, &p.Download, &p.Conns); err != nil {
				return nil, err
			}
			byMin[p.Time] = p
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}

		out := make([]DBTrendPoint, 0, mins)
		for i := 0; i < mins; i++ {
			t := startTime.Add(time.Duration(i) * time.Minute)
			key := t.Format("2006-01-02T15:04:00")
			if p, ok := byMin[key]; ok {
				p.Date = t.Format("2006-01-02")
				out = append(out, p)
			} else {
				out = append(out, DBTrendPoint{Time: key, Date: t.Format("2006-01-02")})
			}
		}
		return out, nil
	}

	startT, err := time.ParseInLocation("2006-01-02", startDate, time.Local)
	if err != nil {
		return nil, fmt.Errorf("invalid start date: %w", err)
	}
	endT, err := time.ParseInLocation("2006-01-02", endDate, time.Local)
	if err != nil {
		return nil, fmt.Errorf("invalid end date: %w", err)
	}
	if endT.Before(startT) {
		return []DBTrendPoint{}, nil
	}

	// Hourly breakdown over [startDate, endDate]
	if granularity == "hour" {
		since := startDate + "T00:00:00"
		until := endDate + "T23:59:59"
		q := `SELECT hour, SUM(upload), SUM(download), SUM(conn_count) FROM hourly_stats WHERE hour >= ? AND hour <= ?`
		args := []interface{}{since, until}
		if user != "" {
			q += ` AND user = ?`
			args = append(args, user)
		}
		q += ` GROUP BY hour ORDER BY hour`

		rows, err := s.readDB.Query(q, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		byHour := make(map[string]DBTrendPoint)
		for rows.Next() {
			var p DBTrendPoint
			if err := rows.Scan(&p.Time, &p.Upload, &p.Download, &p.Conns); err != nil {
				return nil, err
			}
			byHour[p.Time] = p
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}

		var out []DBTrendPoint
		for cur := startT; !cur.After(endT); cur = cur.AddDate(0, 0, 1) {
			dStr := cur.Format("2006-01-02")
			maxH := 23
			if dStr == todayStr {
				maxH = now.Hour()
			}
			for h := 0; h <= maxH; h++ {
				key := fmt.Sprintf("%sT%02d:00:00", dStr, h)
				if p, ok := byHour[key]; ok {
					p.Date = dStr
					out = append(out, p)
				} else {
					out = append(out, DBTrendPoint{Time: key, Date: dStr})
				}
			}
		}
		return out, nil
	}

	// Daily query (used for both "day" and "week" rollup)
	q := `SELECT date, SUM(upload), SUM(download), SUM(conn_count) FROM daily_user_stats WHERE date >= ? AND date <= ?`
	args := []interface{}{startDate, endDate}
	if user != "" {
		q += ` AND user = ?`
		args = append(args, user)
	}
	q += ` GROUP BY date ORDER BY date`

	rows, err := s.readDB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byDate := make(map[string]DBTrendPoint)
	for rows.Next() {
		var p DBTrendPoint
		if err := rows.Scan(&p.Time, &p.Upload, &p.Download, &p.Conns); err != nil {
			return nil, err
		}
		byDate[p.Time] = p
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var daily []DBTrendPoint
	for cur := startT; !cur.After(endT); cur = cur.AddDate(0, 0, 1) {
		dStr := cur.Format("2006-01-02")
		if p, ok := byDate[dStr]; ok {
			p.Date = dStr
			daily = append(daily, p)
		} else {
			daily = append(daily, DBTrendPoint{Time: dStr, Date: dStr})
		}
	}

	if granularity == "week" {
		var rolled []DBTrendPoint
		for i := 0; i < len(daily); i += 7 {
			endIdx := i + 7
			if endIdx > len(daily) {
				endIdx = len(daily)
			}
			chunk := daily[i:endIdx]
			var up, down, conns uint64
			for _, p := range chunk {
				up += p.Upload
				down += p.Download
				conns += p.Conns
			}
			first := chunk[0].Date
			last := chunk[len(chunk)-1].Date
			rolled = append(rolled, DBTrendPoint{
				Time:     first[5:] + "~" + last[5:],
				Date:     last,
				Upload:   up,
				Download: down,
				Conns:    conns,
			})
		}
		return rolled, nil
	}

	return daily, nil
}

// DBCountryStats holds country-level stats.
type DBCountryStats struct {
	Country     string `json:"country"`
	CountryName string `json:"country_name"`
	Continent   string `json:"continent"`
	Upload      uint64 `json:"upload"`
	Download    uint64 `json:"download"`
	ConnCount   uint64 `json:"conn_count"`
}

func (s *StatsDB) GetCountryStats() ([]DBCountryStats, error) {
	rows, err := s.readDB.Query(`SELECT country, COALESCE(MAX(country_name),''), COALESCE(MAX(continent),''), SUM(upload), SUM(download), SUM(conn_count) FROM country_stats GROUP BY country ORDER BY SUM(upload)+SUM(download) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DBCountryStats
	for rows.Next() {
		var c DBCountryStats
		if err := rows.Scan(&c.Country, &c.CountryName, &c.Continent, &c.Upload, &c.Download, &c.ConnCount); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetCountryStatsRange returns country-level stats aggregated over [startDate, endDate] (inclusive).
func (s *StatsDB) GetCountryStatsRange(startDate, endDate, user string) ([]DBCountryStats, error) {
	if startDate == "" || endDate == "" {
		return s.GetCountryStats()
	}

	q := `SELECT country, COALESCE(MAX(country_name),''), COALESCE(MAX(continent),''), COALESCE(SUM(upload),0), COALESCE(SUM(download),0), COALESCE(SUM(conn_count),0)
	      FROM daily_country_stats WHERE date >= ? AND date <= ?`
	args := []interface{}{startDate, endDate}
	if user != "" {
		q += ` AND user = ?`
		args = append(args, user)
	}
	q += ` GROUP BY country ORDER BY SUM(upload) + SUM(download) DESC`

	rows, err := s.readDB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DBCountryStats
	for rows.Next() {
		var c DBCountryStats
		if err := rows.Scan(&c.Country, &c.CountryName, &c.Continent, &c.Upload, &c.Download, &c.ConnCount); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// User management helpers (disable/enable with lock-free/in-memory read path)
// ---------------------------------------------------------------------------

func (s *StatsDB) SetUserDisabled(username string, disabled bool) error {
	val := 0
	if disabled {
		val = 1
	}
	_, err := s.db.Exec(`INSERT INTO user_stats (username, disabled, first_seen, last_access) VALUES (?, ?, datetime('now'), datetime('now')) ON CONFLICT(username) DO UPDATE SET disabled=excluded.disabled`, username, val)
	if err != nil {
		return err
	}

	s.disabledMu.Lock()
	if disabled {
		s.disabledUsers[username] = true
	} else {
		delete(s.disabledUsers, username)
	}
	s.disabledMu.Unlock()
	return nil
}

// IsUserDisabled checks the in-memory disabled-user cache so proxy requests
// never touch SQLite on the hot path.
func (s *StatsDB) IsUserDisabled(username string) bool {
	s.disabledMu.RLock()
	disabled := s.disabledUsers[username]
	s.disabledMu.RUnlock()
	return disabled
}

func (s *StatsDB) IncrementRequestCount(username string) {
	now := time.Now()
	ts := now.Format(time.RFC3339)
	date := now.Format("2006-01-02")
	_, _ = s.db.Exec(`INSERT INTO user_stats (username, request_count, first_seen, last_access) VALUES (?, 1, ?, ?) ON CONFLICT(username) DO UPDATE SET request_count=request_count+1, last_access=excluded.last_access`, username, ts, ts)
	_, _ = s.db.Exec(`INSERT INTO daily_user_stats (date, user, request_count) VALUES (?, ?, 1) ON CONFLICT(date, user) DO UPDATE SET request_count=request_count+1`, date, username)
}

// ---------------------------------------------------------------------------
// Data retention – cleanup old minute/hourly/daily stats
// ---------------------------------------------------------------------------

func (s *StatsDB) CleanupOldData(minuteDays, hourlyDays, dailyDays int) {
	now := time.Now()
	var delMin, delHour, delDailyUser, delDailyDomain, delDailyCountry int64

	if minuteDays > 0 {
		minuteCutoff := now.AddDate(0, 0, -minuteDays).Format("2006-01-02T15:04:00")
		if res, err := s.db.Exec(`DELETE FROM minute_stats WHERE minute < ?`, minuteCutoff); err == nil {
			delMin, _ = res.RowsAffected()
		}
	}

	if hourlyDays > 0 {
		hourlyCutoff := now.AddDate(0, 0, -hourlyDays).Format("2006-01-02T15:00:00")
		if res, err := s.db.Exec(`DELETE FROM hourly_stats WHERE hour < ?`, hourlyCutoff); err == nil {
			delHour, _ = res.RowsAffected()
		}
	}

	if dailyDays > 0 {
		// Keep [today-(dailyDays-1), today] inclusive (e.g. 90 days total).
		dailyCutoff := now.AddDate(0, 0, -(dailyDays - 1)).Format("2006-01-02")
		if res, err := s.db.Exec(`DELETE FROM daily_user_stats WHERE date < ?`, dailyCutoff); err == nil {
			delDailyUser, _ = res.RowsAffected()
		}
		if res, err := s.db.Exec(`DELETE FROM daily_domain_stats WHERE date < ?`, dailyCutoff); err == nil {
			delDailyDomain, _ = res.RowsAffected()
		}
		if res, err := s.db.Exec(`DELETE FROM daily_country_stats WHERE date < ?`, dailyCutoff); err == nil {
			delDailyCountry, _ = res.RowsAffected()
		}
	}

	totalDeleted := delMin + delHour + delDailyUser + delDailyDomain + delDailyCountry
	if totalDeleted > 0 {
		log.Printf("[DB] Cleanup: deleted %d minute, %d hourly, %d daily_user, %d daily_domain, %d daily_country rows",
			delMin, delHour, delDailyUser, delDailyDomain, delDailyCountry)
		_, _ = s.db.Exec(`PRAGMA incremental_vacuum(200)`)
		_, _ = s.db.Exec(`PRAGMA wal_checkpoint(PASSIVE)`)
	}
}

// MigrateFromJSON imports legacy JSON stats into SQLite.
func (s *StatsDB) MigrateFromJSON(userStats map[string]*UserStats) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO user_stats (username, total_upload, total_download, conn_count, request_count, first_seen, last_access, disabled)
		VALUES (?, 0, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(username) DO UPDATE SET
			total_download = total_download + excluded.total_download,
			conn_count     = conn_count + excluded.conn_count,
			request_count  = request_count + excluded.request_count,
			first_seen     = MIN(first_seen, excluded.first_seen),
			last_access    = MAX(last_access, excluded.last_access),
			disabled       = excluded.disabled`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, us := range userStats {
		dis := 0
		if us.Disabled {
			dis = 1
		}
		if _, err := stmt.Exec(us.Username, us.TotalBytes, us.ConnectionCount, us.RequestsCount,
			us.ConnectedSince.Format(time.RFC3339), us.LastAccess.Format(time.RFC3339), dis); err != nil {
			return fmt.Errorf("migrate user %s: %w", us.Username, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return s.loadDisabledUsersCache()
}
