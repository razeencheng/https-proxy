package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// StatsDB wraps the SQLite database for traffic statistics storage.
type StatsDB struct {
	db *sql.DB
}

// NewStatsDB opens (or creates) a SQLite database at dbPath and initialises
// all required tables and indexes.
func NewStatsDB(dbPath string) (*StatsDB, error) {
	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Performance pragmas
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA cache_size=-16000", // 16 MB page cache
		"PRAGMA busy_timeout=5000",
		"PRAGMA temp_store=MEMORY",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("exec %s: %w", pragma, err)
		}
	}

	sdb := &StatsDB{db: db}
	if err := sdb.initTables(); err != nil {
		db.Close()
		return nil, err
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
		`CREATE TABLE IF NOT EXISTS hourly_stats (
			user       TEXT NOT NULL,
			hour       TEXT NOT NULL,
			upload     INTEGER DEFAULT 0,
			download   INTEGER DEFAULT 0,
			conn_count INTEGER DEFAULT 0,
			PRIMARY KEY (user, hour)
		)`,
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

// Close closes the database connection.
func (s *StatsDB) Close() error {
	return s.db.Close()
}

// ---------------------------------------------------------------------------
// Batch upsert helpers – called from StatsCollector.flush()
// ---------------------------------------------------------------------------

// TrafficRecord holds a single aggregated traffic record ready for DB upsert.
type TrafficRecord struct {
	Username    string
	Domain      string
	Upload      uint64
	Download    uint64
	ConnCount   int
	Country     string
	CountryName string
	Continent   string
	Minute      string // "2006-01-02T15:04:00"
	Hour        string // "2006-01-02T15:00:00"
	Timestamp   time.Time
}

// BatchUpsert writes a slice of TrafficRecords into all stat tables inside a
// single transaction for maximum throughput.
func (s *StatsDB) BatchUpsert(records []TrafficRecord) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint: will be committed below

	stmtUser, err := tx.Prepare(`INSERT INTO user_stats (username, total_upload, total_download, conn_count, first_seen, last_access)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(username) DO UPDATE SET
			total_upload   = total_upload   + excluded.total_upload,
			total_download = total_download + excluded.total_download,
			conn_count     = conn_count     + excluded.conn_count,
			last_access    = excluded.last_access`)
	if err != nil {
		return fmt.Errorf("prepare user_stats: %w", err)
	}
	defer stmtUser.Close()

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

	stmtCountry, err := tx.Prepare(`INSERT INTO country_stats (user, country, country_name, continent, upload, download, conn_count, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user, country) DO UPDATE SET
			country_name = COALESCE(excluded.country_name, country_name),
			continent    = COALESCE(excluded.continent, continent),
			upload       = upload     + excluded.upload,
			download     = download   + excluded.download,
			conn_count   = conn_count + excluded.conn_count,
			last_seen    = excluded.last_seen`)
	if err != nil {
		return fmt.Errorf("prepare country_stats: %w", err)
	}
	defer stmtCountry.Close()

	for _, r := range records {
		ts := r.Timestamp.Format(time.RFC3339)

		if _, err := stmtUser.Exec(r.Username, r.Upload, r.Download, r.ConnCount, ts, ts); err != nil {
			return fmt.Errorf("exec user_stats (%s): %w", r.Username, err)
		}

		if r.Domain != "" {
			if _, err := stmtDomain.Exec(r.Username, r.Domain, r.Upload, r.Download, r.ConnCount, ts); err != nil {
				return fmt.Errorf("exec domain_stats (%s/%s): %w", r.Username, r.Domain, err)
			}
		}

		if r.Minute != "" {
			if _, err := stmtMinute.Exec(r.Username, r.Minute, r.Upload, r.Download, r.ConnCount); err != nil {
				return fmt.Errorf("exec minute_stats: %w", err)
			}
		}

		if r.Hour != "" {
			if _, err := stmtHour.Exec(r.Username, r.Hour, r.Upload, r.Download, r.ConnCount); err != nil {
				return fmt.Errorf("exec hourly_stats: %w", err)
			}
		}

		if r.Country != "" {
			if _, err := stmtCountry.Exec(r.Username, r.Country, r.CountryName, r.Continent, r.Upload, r.Download, r.ConnCount, ts); err != nil {
				return fmt.Errorf("exec country_stats: %w", err)
			}
		}
	}

	return tx.Commit()
}

// ---------------------------------------------------------------------------
// Query helpers – used by API layer
// ---------------------------------------------------------------------------

// OverviewStats holds global overview numbers.
type OverviewStats struct {
	TotalUpload   uint64 `json:"total_upload"`
	TotalDownload uint64 `json:"total_download"`
	TotalConns    uint64 `json:"total_connections"`
	DomainCount   int    `json:"domain_count"`
	UserCount     int    `json:"user_count"`
	CountryCount  int    `json:"country_count"`
}

func (s *StatsDB) GetOverview() (*OverviewStats, error) {
	o := &OverviewStats{}
	err := s.db.QueryRow(`SELECT COALESCE(SUM(total_upload),0), COALESCE(SUM(total_download),0), COALESCE(SUM(conn_count),0), COUNT(*) FROM user_stats`).
		Scan(&o.TotalUpload, &o.TotalDownload, &o.TotalConns, &o.UserCount)
	if err != nil {
		return nil, err
	}
	s.db.QueryRow(`SELECT COUNT(DISTINCT domain) FROM domain_stats`).Scan(&o.DomainCount)
	s.db.QueryRow(`SELECT COUNT(DISTINCT country) FROM country_stats`).Scan(&o.CountryCount)
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
	rows, err := s.db.Query(`SELECT username, total_upload, total_download, conn_count, request_count, COALESCE(first_seen,''), COALESCE(last_access,''), disabled FROM user_stats ORDER BY total_upload+total_download DESC`)
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
	err := s.db.QueryRow(`SELECT username, total_upload, total_download, conn_count, request_count, COALESCE(first_seen,''), COALESCE(last_access,''), disabled FROM user_stats WHERE username=?`, username).
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
	LastSeen  string `json:"last_seen"`
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

	rows, err := s.db.Query(q, args...)
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

// DBTrendPoint holds a single time-series data point.
type DBTrendPoint struct {
	Time     string `json:"time"`
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

	// Use the appropriate time column name based on table
	timeCol := "minute"
	if table == "hourly_stats" {
		timeCol = "hour"
	}

	q := fmt.Sprintf(`SELECT %s, SUM(upload), SUM(download), SUM(conn_count) FROM %s WHERE %s>=? GROUP BY %s ORDER BY %s`, timeCol, table, timeCol, timeCol, timeCol)
	rows, err := s.db.Query(q, since)
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
	rows, err := s.db.Query(`SELECT country, COALESCE(country_name,''), COALESCE(continent,''), SUM(upload), SUM(download), SUM(conn_count) FROM country_stats GROUP BY country ORDER BY SUM(upload)+SUM(download) DESC`)
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
// User management helpers (disable/enable)
// ---------------------------------------------------------------------------

func (s *StatsDB) SetUserDisabled(username string, disabled bool) error {
	val := 0
	if disabled {
		val = 1
	}
	_, err := s.db.Exec(`INSERT INTO user_stats (username, disabled, first_seen, last_access) VALUES (?, ?, datetime('now'), datetime('now')) ON CONFLICT(username) DO UPDATE SET disabled=excluded.disabled`, username, val)
	return err
}

func (s *StatsDB) IsUserDisabled(username string) bool {
	var dis int
	err := s.db.QueryRow(`SELECT disabled FROM user_stats WHERE username=?`, username).Scan(&dis)
	if err != nil {
		return false
	}
	return dis != 0
}

func (s *StatsDB) IncrementRequestCount(username string) {
	s.db.Exec(`INSERT INTO user_stats (username, request_count, first_seen, last_access) VALUES (?, 1, datetime('now'), datetime('now')) ON CONFLICT(username) DO UPDATE SET request_count=request_count+1, last_access=datetime('now')`, username)
}

// ---------------------------------------------------------------------------
// Data retention – cleanup old minute/hourly stats
// ---------------------------------------------------------------------------

func (s *StatsDB) CleanupOldData(minuteDays, hourlyDays int) {
	minuteCutoff := time.Now().AddDate(0, 0, -minuteDays).Format("2006-01-02T15:04:00")
	hourlyCutoff := time.Now().AddDate(0, 0, -hourlyDays).Format("2006-01-02T15:00:00")

	res1, _ := s.db.Exec(`DELETE FROM minute_stats WHERE minute < ?`, minuteCutoff)
	res2, _ := s.db.Exec(`DELETE FROM hourly_stats WHERE hour < ?`, hourlyCutoff)

	del1, _ := res1.RowsAffected()
	del2, _ := res2.RowsAffected()
	if del1 > 0 || del2 > 0 {
		log.Printf("[DB] Cleanup: deleted %d minute rows, %d hourly rows", del1, del2)
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
		// Legacy stats only tracked total bytes (not separated upload/download),
		// so we put everything into download as a best-effort migration.
		if _, err := stmt.Exec(us.Username, us.TotalBytes, us.ConnectionCount, us.RequestsCount,
			us.ConnectedSince.Format(time.RFC3339), us.LastAccess.Format(time.RFC3339), dis); err != nil {
			return fmt.Errorf("migrate user %s: %w", us.Username, err)
		}
	}
	return tx.Commit()
}
