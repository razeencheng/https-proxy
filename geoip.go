package main

import (
	"log"
	"net"
	"sync"

	"github.com/oschwald/geoip2-golang"
)

// GeoIPService provides country lookup from IP addresses using MaxMind GeoLite2.
type GeoIPService struct {
	mu     sync.RWMutex
	reader *geoip2.Reader
}

// GeoResult holds the result of a GeoIP lookup.
type GeoResult struct {
	Country     string // ISO 3166-1 alpha-2 (e.g. "US")
	CountryName string // English name   (e.g. "United States")
	Continent   string // Continent code (e.g. "NA")
}

// NewGeoIPService opens the MaxMind GeoLite2 database at dbPath.
// If dbPath is empty or the file cannot be opened, a nil-safe service is
// returned that always produces empty results (GeoIP is optional).
func NewGeoIPService(dbPath string) *GeoIPService {
	if dbPath == "" {
		log.Println("[GeoIP] No database path configured, GeoIP disabled")
		return &GeoIPService{}
	}

	reader, err := geoip2.Open(dbPath)
	if err != nil {
		log.Printf("[GeoIP] Failed to open database %s: %v (GeoIP disabled)", dbPath, err)
		return &GeoIPService{}
	}
	log.Printf("[GeoIP] Loaded database: %s", dbPath)
	return &GeoIPService{reader: reader}
}

// Lookup resolves an IP string to a GeoResult.
// Returns nil if GeoIP is disabled or the lookup fails.
func (g *GeoIPService) Lookup(ipStr string) *GeoResult {
	if g == nil || g.reader == nil {
		return nil
	}

	ip := net.ParseIP(ipStr)
	if ip == nil {
		return nil
	}

	g.mu.RLock()
	defer g.mu.RUnlock()

	record, err := g.reader.Country(ip)
	if err != nil {
		return nil
	}

	return &GeoResult{
		Country:     record.Country.IsoCode,
		CountryName: record.Country.Names["en"],
		Continent:   record.Continent.Code,
	}
}

// Close releases the GeoIP database resources.
func (g *GeoIPService) Close() {
	if g != nil && g.reader != nil {
		g.reader.Close()
	}
}
