package main

import (
	"log"
	"sync"
	"time"
)

// TrafficEvent is emitted when a CONNECT tunnel closes, carrying per-connection
// traffic information with domain and directional byte counts.
type TrafficEvent struct {
	Username    string
	Domain      string
	TargetIP    string
	Upload      uint64
	Download    uint64
	Timestamp   time.Time
	Country     string
	CountryName string
	Continent   string
}

// bufferKey uniquely identifies an aggregation bucket.
type bufferKey struct {
	Username string
	Domain   string
	Country  string
	Minute   string
	Hour     string
}

// aggregatedEvent accumulates traffic within a single buffer key.
type aggregatedEvent struct {
	Upload      uint64
	Download    uint64
	ConnCount   int
	CountryName string
	Continent   string
	LastSeen    time.Time
}

// StatsCollector receives TrafficEvents asynchronously, aggregates them in
// memory, and periodically flushes to SQLite via StatsDB.BatchUpsert.
type StatsCollector struct {
	db      *StatsDB
	geoIP   *GeoIPService
	eventCh chan TrafficEvent

	mu     sync.Mutex
	buffer map[bufferKey]*aggregatedEvent

	flushInterval time.Duration
	maxBuffer     int
	done          chan struct{}
	wg            sync.WaitGroup
}

// NewStatsCollector creates a new collector. flushSeconds controls
// how often the buffer is written to disk (default 30s).
func NewStatsCollector(db *StatsDB, geoIP *GeoIPService, flushSeconds int) *StatsCollector {
	if flushSeconds <= 0 {
		flushSeconds = 30
	}
	sc := &StatsCollector{
		db:            db,
		geoIP:         geoIP,
		eventCh:       make(chan TrafficEvent, 10000),
		buffer:        make(map[bufferKey]*aggregatedEvent),
		flushInterval: time.Duration(flushSeconds) * time.Second,
		maxBuffer:     5000,
		done:          make(chan struct{}),
	}
	sc.wg.Add(1)
	go sc.loop()
	return sc
}

// Record sends a TrafficEvent into the collector. Non-blocking – if the
// channel is full the event is silently dropped (and logged).
func (sc *StatsCollector) Record(ev TrafficEvent) {
	// Enrich with GeoIP if not already set
	if ev.Country == "" && ev.TargetIP != "" && sc.geoIP != nil {
		if geo := sc.geoIP.Lookup(ev.TargetIP); geo != nil {
			ev.Country = geo.Country
			ev.CountryName = geo.CountryName
			ev.Continent = geo.Continent
		}
	}

	select {
	case sc.eventCh <- ev:
	default:
		log.Printf("[StatsCollector] Channel full, dropping event for %s/%s", ev.Username, ev.Domain)
	}
}

// Stop flushes remaining data and shuts down the background goroutine.
func (sc *StatsCollector) Stop() {
	close(sc.done)
	sc.wg.Wait()
}

func (sc *StatsCollector) loop() {
	defer sc.wg.Done()
	ticker := time.NewTicker(sc.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case ev := <-sc.eventCh:
			sc.aggregate(ev)
			// Force flush if buffer is too large
			if sc.bufferLen() >= sc.maxBuffer {
				sc.flush()
			}
		case <-ticker.C:
			sc.flush()
		case <-sc.done:
			// Drain remaining events
			for {
				select {
				case ev := <-sc.eventCh:
					sc.aggregate(ev)
				default:
					sc.flush()
					return
				}
			}
		}
	}
}

func (sc *StatsCollector) aggregate(ev TrafficEvent) {
	t := ev.Timestamp
	if t.IsZero() {
		t = time.Now()
	}
	minute := t.Truncate(time.Minute).Format("2006-01-02T15:04:00")
	hour := t.Truncate(time.Hour).Format("2006-01-02T15:00:00")

	key := bufferKey{
		Username: ev.Username,
		Domain:   ev.Domain,
		Country:  ev.Country,
		Minute:   minute,
		Hour:     hour,
	}

	sc.mu.Lock()
	defer sc.mu.Unlock()

	agg, ok := sc.buffer[key]
	if !ok {
		agg = &aggregatedEvent{
			CountryName: ev.CountryName,
			Continent:   ev.Continent,
		}
		sc.buffer[key] = agg
	}
	agg.Upload += ev.Upload
	agg.Download += ev.Download
	agg.ConnCount++
	if t.After(agg.LastSeen) {
		agg.LastSeen = t
	}
}

func (sc *StatsCollector) bufferLen() int {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return len(sc.buffer)
}

func (sc *StatsCollector) flush() {
	sc.mu.Lock()
	if len(sc.buffer) == 0 {
		sc.mu.Unlock()
		return
	}
	// Swap out the buffer under lock
	buf := sc.buffer
	sc.buffer = make(map[bufferKey]*aggregatedEvent, len(buf))
	sc.mu.Unlock()

	records := make([]TrafficRecord, 0, len(buf))
	for key, agg := range buf {
		records = append(records, TrafficRecord{
			Username:    key.Username,
			Domain:      key.Domain,
			Upload:      agg.Upload,
			Download:    agg.Download,
			ConnCount:   agg.ConnCount,
			Country:     key.Country,
			CountryName: agg.CountryName,
			Continent:   agg.Continent,
			Minute:      key.Minute,
			Hour:        key.Hour,
			Timestamp:   agg.LastSeen,
		})
	}

	if err := sc.db.BatchUpsert(records); err != nil {
		log.Printf("[StatsCollector] Flush error: %v (will retry next cycle)", err)
		// Re-add to buffer so data isn't lost
		sc.mu.Lock()
		for key, agg := range buf {
			if existing, ok := sc.buffer[key]; ok {
				existing.Upload += agg.Upload
				existing.Download += agg.Download
				existing.ConnCount += agg.ConnCount
			} else {
				sc.buffer[key] = agg
			}
		}
		sc.mu.Unlock()
	}
}
