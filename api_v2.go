package main

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// registerV2API registers all v2 REST API routes on the given mux.
// The StatsDB must be non-nil; if it is nil the routes will return 503.
func registerV2API(mux *http.ServeMux, statsDB *StatsDB) {
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

	mux.HandleFunc("/api/v2/overview", check(func(w http.ResponseWriter, r *http.Request) {
		overview, err := statsDB.GetOverview()
		if err != nil {
			writeJSONResponse(w, WebResponse{Success: false, Error: err.Error()}, http.StatusInternalServerError)
			return
		}
		writeJSONResponse(w, WebResponse{Success: true, Data: overview}, http.StatusOK)
	}))

	mux.HandleFunc("/api/v2/users", check(func(w http.ResponseWriter, r *http.Request) {
		users, err := statsDB.GetAllUsers()
		if err != nil {
			writeJSONResponse(w, WebResponse{Success: false, Error: err.Error()}, http.StatusInternalServerError)
			return
		}
		writeJSONResponse(w, WebResponse{Success: true, Data: users}, http.StatusOK)
	}))

	mux.HandleFunc("/api/v2/users/", check(func(w http.ResponseWriter, r *http.Request) {
		// Extract username from path: /api/v2/users/{username}
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
		limit := 50
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 {
				limit = n
			}
		}
		user := r.URL.Query().Get("user")
		domains, err := statsDB.GetTopDomains(limit, user)
		if err != nil {
			writeJSONResponse(w, WebResponse{Success: false, Error: err.Error()}, http.StatusInternalServerError)
			return
		}
		writeJSONResponse(w, WebResponse{Success: true, Data: domains}, http.StatusOK)
	}))

	mux.HandleFunc("/api/v2/trends", check(func(w http.ResponseWriter, r *http.Request) {
		rangeStr := r.URL.Query().Get("range")
		if rangeStr == "" {
			rangeStr = "1h"
		}
		trends, err := statsDB.GetTrends(rangeStr)
		if err != nil {
			writeJSONResponse(w, WebResponse{Success: false, Error: err.Error()}, http.StatusInternalServerError)
			return
		}
		writeJSONResponse(w, WebResponse{Success: true, Data: trends}, http.StatusOK)
	}))

	mux.HandleFunc("/api/v2/countries", check(func(w http.ResponseWriter, r *http.Request) {
		countries, err := statsDB.GetCountryStats()
		if err != nil {
			writeJSONResponse(w, WebResponse{Success: false, Error: err.Error()}, http.StatusInternalServerError)
			return
		}
		writeJSONResponse(w, WebResponse{Success: true, Data: countries}, http.StatusOK)
	}))
}

// writeJSONResponseV2 is a helper that sets JSON content type and writes body.
// We reuse writeJSONResponse from admin.go, but define an alias for clarity.
func writeJSONResponseV2(w http.ResponseWriter, data interface{}, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}
