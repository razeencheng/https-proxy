package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// registerV2API registers all v2 REST API routes on the given mux.
// The StatsDB must be non-nil; if it is nil the routes will return 503.
func registerV2API(mux *http.ServeMux, statsDB *StatsDB, retentionDaysOpts ...int) {
	retentionDays := 90
	if len(retentionDaysOpts) > 0 && retentionDaysOpts[0] > 0 {
		retentionDays = retentionDaysOpts[0]
	}

	// Wrapper that checks StatsDB availability
	check := func(handler func(w http.ResponseWriter, r *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if statsDB == nil {
				writeJSONResponse(w, WebResponse{Success: false, Error: "Stats database not available"}, http.StatusServiceUnavailable)
				return
			}
			handler(w, r)
		}
	}

	// Metadata endpoint: informs dashboard of server local date & retention window
	mux.HandleFunc("/api/v2/meta", check(func(w http.ResponseWriter, r *http.Request) {
		now := time.Now()
		today := now.Format("2006-01-02")
		minDate := now.AddDate(0, 0, -(retentionDays - 1)).Format("2006-01-02")
		writeJSONResponse(w, WebResponse{
			Success: true,
			Data: map[string]interface{}{
				"retention_days": retentionDays,
				"today":          today,
				"min_date":       minDate,
				"server_time":    now.Format(time.RFC3339),
			},
		}, http.StatusOK)
	}))

	mux.HandleFunc("/api/v2/overview", check(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		start := q.Get("start")
		end := q.Get("end")
		user := q.Get("user")
		compare := q.Get("compare") == "1" || q.Get("compare") == "true"

		var (
			overview *OverviewStats
			err      error
		)
		if start != "" && end != "" {
			overview, err = statsDB.GetOverviewRange(start, end, user)
			if err == nil && compare {
				if prevStart, prevEnd, ok := computePreviousRange(start, end, retentionDays); ok {
					if prev, prevErr := statsDB.GetOverviewRange(prevStart, prevEnd, user); prevErr == nil {
						overview.Previous = prev
					}
				}
			}
		} else {
			overview, err = statsDB.GetOverview()
		}

		if err != nil {
			writeJSONResponse(w, WebResponse{Success: false, Error: err.Error()}, http.StatusInternalServerError)
			return
		}
		writeJSONResponse(w, WebResponse{Success: true, Data: overview}, http.StatusOK)
	}))

	mux.HandleFunc("/api/v2/users", check(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		start := q.Get("start")
		end := q.Get("end")

		var (
			users []DBUserStats
			err   error
		)
		if start != "" && end != "" {
			users, err = statsDB.GetUsersRange(start, end)
		} else {
			users, err = statsDB.GetAllUsers()
		}
		if err != nil {
			writeJSONResponse(w, WebResponse{Success: false, Error: err.Error()}, http.StatusInternalServerError)
			return
		}
		writeJSONResponse(w, WebResponse{Success: true, Data: users}, http.StatusOK)
	}))

	mux.HandleFunc("/api/v2/users/", check(func(w http.ResponseWriter, r *http.Request) {
		username := r.URL.Path[len("/api/v2/users/"):]
		if username == "" {
			writeJSONResponse(w, WebResponse{Success: false, Error: "username required"}, http.StatusBadRequest)
			return
		}
		user, err := statsDB.GetUser(username)
		if err != nil {
			writeJSONResponse(w, WebResponse{Success: false, Error: "user not found"}, http.StatusNotFound)
			return
		}
		writeJSONResponse(w, WebResponse{Success: true, Data: user}, http.StatusOK)
	}))

	mux.HandleFunc("/api/v2/domains", check(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		limit := 50
		if l := q.Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 {
				limit = n
			}
		}
		user := q.Get("user")
		start := q.Get("start")
		end := q.Get("end")
		search := q.Get("q")

		var (
			domains []DBDomainStats
			err     error
		)
		if start != "" && end != "" {
			domains, err = statsDB.GetTopDomainsRange(start, end, limit, user, search)
		} else {
			domains, err = statsDB.GetTopDomains(limit, user)
		}
		if err != nil {
			writeJSONResponse(w, WebResponse{Success: false, Error: err.Error()}, http.StatusInternalServerError)
			return
		}
		writeJSONResponse(w, WebResponse{Success: true, Data: domains}, http.StatusOK)
	}))

	mux.HandleFunc("/api/v2/trends", check(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		start := q.Get("start")
		end := q.Get("end")
		granularity := q.Get("granularity")
		user := q.Get("user")

		var (
			trends []DBTrendPoint
			err    error
		)
		if granularity != "" || (start != "" && end != "") {
			if granularity == "" {
				granularity = "day"
			}
			trends, err = statsDB.GetTrendsRange(start, end, granularity, user)
		} else {
			rangeStr := q.Get("range")
			if rangeStr == "" {
				rangeStr = "1h"
			}
			trends, err = statsDB.GetTrends(rangeStr)
		}
		if err != nil {
			writeJSONResponse(w, WebResponse{Success: false, Error: err.Error()}, http.StatusInternalServerError)
			return
		}
		writeJSONResponse(w, WebResponse{Success: true, Data: trends}, http.StatusOK)
	}))

	mux.HandleFunc("/api/v2/countries", check(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		start := q.Get("start")
		end := q.Get("end")
		user := q.Get("user")

		var (
			countries []DBCountryStats
			err       error
		)
		if start != "" && end != "" {
			countries, err = statsDB.GetCountryStatsRange(start, end, user)
		} else {
			countries, err = statsDB.GetCountryStats()
		}
		if err != nil {
			writeJSONResponse(w, WebResponse{Success: false, Error: err.Error()}, http.StatusInternalServerError)
			return
		}
		writeJSONResponse(w, WebResponse{Success: true, Data: countries}, http.StatusOK)
	}))
}

// computePreviousRange returns the immediately preceding equal-span rolling date
// window [prevStart, prevEnd] if it falls entirely within the retention window.
func computePreviousRange(startStr, endStr string, retentionDays int) (string, string, bool) {
	startT, err1 := time.ParseInLocation("2006-01-02", startStr, time.Local)
	endT, err2 := time.ParseInLocation("2006-01-02", endStr, time.Local)
	if err1 != nil || err2 != nil || endT.Before(startT) {
		return "", "", false
	}

	spanDays := int(endT.Sub(startT).Hours()/24) + 1
	if spanDays >= retentionDays {
		return "", "", false
	}

	minDate := time.Now().AddDate(0, 0, -(retentionDays - 1)).Format("2006-01-02")
	prevEnd := startT.AddDate(0, 0, -1)
	prevStart := prevEnd.AddDate(0, 0, -(spanDays - 1))
	if prevStart.Format("2006-01-02") < minDate {
		return "", "", false
	}

	return prevStart.Format("2006-01-02"), prevEnd.Format("2006-01-02"), true
}

// writeJSONResponseV2 is a helper that sets JSON content type and writes body.
func writeJSONResponseV2(w http.ResponseWriter, data interface{}, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}
