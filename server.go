package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Method    string `json:"method"`
	URL       string `json:"url"`
	Body      string `json:"body,omitempty"`
}

type ModuleCounts struct {
	Locations int `json:"locations"`
	Sessions  int `json:"sessions"`
	CDRs      int `json:"cdrs"`
	Tariffs   int `json:"tariffs"`
	Tokens    int `json:"tokens"`
}

type ServerState struct {
	mu             sync.RWMutex
	CPOCredentials map[string]any
	Locations      map[string]map[string]any
	Sessions       map[string]map[string]any
	CDRs           map[string]map[string]any
	Tariffs        map[string]map[string]any
	Tokens         map[string]map[string]any
	Counts         ModuleCounts
}

func (s *ServerState) Store(module string) map[string]map[string]any {
	switch module {
	case "locations":
		return s.Locations
	case "sessions":
		return s.Sessions
	case "cdrs":
		return s.CDRs
	case "tariffs":
		return s.Tariffs
	case "tokens":
		return s.Tokens
	}
	return nil
}

func (s *ServerState) CountFor(module string) int {
	switch module {
	case "locations":
		return s.Counts.Locations
	case "sessions":
		return s.Counts.Sessions
	case "cdrs":
		return s.Counts.CDRs
	case "tariffs":
		return s.Counts.Tariffs
	case "tokens":
		return s.Counts.Tokens
	}
	return 0
}

func (s *ServerState) bumpCount(module string, delta int) {
	switch module {
	case "locations":
		s.Counts.Locations += delta
	case "sessions":
		s.Counts.Sessions += delta
	case "cdrs":
		s.Counts.CDRs += delta
	case "tariffs":
		s.Counts.Tariffs += delta
	case "tokens":
		s.Counts.Tokens += delta
	}
}

type OnLog func(LogEntry)
type OnStateChange func()

type Server struct {
	URL           string
	Port          int
	State         *ServerState
	OnLog         OnLog
	OnStateChange OnStateChange
	mux           *http.ServeMux
	httpServer    *http.Server
}

func NewServer(url string, port int, onLog OnLog, onStateChange OnStateChange) *Server {
	state := &ServerState{
		Locations: map[string]map[string]any{},
		Sessions:  map[string]map[string]any{},
		CDRs:      map[string]map[string]any{},
		Tariffs:   map[string]map[string]any{},
		Tokens: map[string]map[string]any{
			"valid-token-1": {
				"uid":           "valid-token-1",
				"type":          "RFID",
				"auth_id":       "NL-MFC-valid-token-1",
				"visual_number": "NL-MFC-000001",
				"issuer":        "Mock MSP",
				"valid":         true,
				"whitelist":     "ALLOWED",
				"last_updated":  "2025-01-01T00:00:00Z",
			},
			"valid-token-2": {
				"uid":           "valid-token-2",
				"type":          "RFID",
				"auth_id":       "NL-MFC-valid-token-2",
				"visual_number": "NL-MFC-000002",
				"issuer":        "Mock MFC",
				"valid":         true,
				"whitelist":     "ALLOWED",
				"last_updated":  "2025-01-01T00:00:00Z",
			},
		},
		Counts: ModuleCounts{Tokens: 2},
	}

	s := &Server{
		URL:           url,
		Port:          port,
		State:         state,
		OnLog:         onLog,
		OnStateChange: onStateChange,
	}
	s.routes()
	return s
}

func nowISO() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

func ocpiResponse(data any, code int, message string) map[string]any {
	return map[string]any{
		"data":           data,
		"status_code":    code,
		"status_message": message,
		"timestamp":      nowISO(),
	}
}

func okResponse(data any) map[string]any {
	return ocpiResponse(data, 1000, "Success")
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (s *Server) logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entry := LogEntry{
			Timestamp: nowISO(),
			Method:    r.Method,
			URL:       r.URL.RequestURI(),
		}
		if r.Body != nil {
			raw, err := io.ReadAll(r.Body)
			_ = r.Body.Close()
			if err == nil && len(bytes.TrimSpace(raw)) > 0 {
				entry.Body = string(raw)
			}
			r.Body = io.NopCloser(bytes.NewReader(raw))
		}
		s.OnLog(entry)
		next.ServeHTTP(w, r)
	})
}

func decodeBody(r *http.Request, v any) error {
	if r.Body == nil {
		return fmt.Errorf("empty body")
	}
	return json.NewDecoder(r.Body).Decode(v)
}

func (s *Server) routes() {
	mux := http.NewServeMux()

	// Versions
	mux.HandleFunc("GET /ocpi/versions", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, okResponse([]map[string]any{
			{"version": "2.1.1", "url": s.URL + "/ocpi/2.1.1"},
		}))
	})

	mux.HandleFunc("GET /ocpi/2.1.1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, okResponse(map[string]any{
			"version": "2.1.1",
			"endpoints": []map[string]any{
				{"identifier": "credentials", "url": s.URL + "/ocpi/2.1.1/credentials"},
				{"identifier": "locations", "url": s.URL + "/ocpi/receiver/2.1.1/locations"},
				{"identifier": "sessions", "url": s.URL + "/ocpi/receiver/2.1.1/sessions"},
				{"identifier": "cdrs", "url": s.URL + "/ocpi/receiver/2.1.1/cdrs"},
				{"identifier": "tariffs", "url": s.URL + "/ocpi/receiver/2.1.1/tariffs"},
				{"identifier": "tokens", "url": s.URL + "/ocpi/sender/2.1.1/tokens"},
				{"identifier": "commands", "url": s.URL + "/ocpi/receiver/2.1.1/commands"},
			},
		}))
	})

	// Credentials
	mspCredentials := func() map[string]any {
		return map[string]any{
			"token":            "mocked-msp-token",
			"url":              s.URL + "/ocpi/versions",
			"business_details": map[string]any{"name": "Mock MFC"},
			"party_id":         "MFC",
			"country_code":     "NL",
		}
	}

	mux.HandleFunc("GET /ocpi/2.1.1/credentials", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, okResponse(mspCredentials()))
	})

	mux.HandleFunc("PUT /ocpi/2.1.1/credentials", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := decodeBody(r, &body); err != nil || body["token"] == nil {
			writeJSON(w, 400, ocpiResponse(nil, 2001, "Token is required"))
			return
		}
		s.State.mu.Lock()
		s.State.CPOCredentials = body
		s.State.mu.Unlock()
		s.OnStateChange()
		writeJSON(w, 200, okResponse(mspCredentials()))
	})

	mux.HandleFunc("DELETE /ocpi/2.1.1/credentials", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, okResponse(nil))
	})

	// Tokens (sender)
	mux.HandleFunc("GET /ocpi/sender/2.1.1/tokens", func(w http.ResponseWriter, r *http.Request) {
		s.State.mu.RLock()
		list := make([]map[string]any, 0, len(s.State.Tokens))
		for _, t := range s.State.Tokens {
			list = append(list, t)
		}
		s.State.mu.RUnlock()
		writeJSON(w, 200, okResponse(list))
	})

	mux.HandleFunc("GET /ocpi/sender/2.1.1/tokens/{countryCode}/{partyId}/{tokenUid}", func(w http.ResponseWriter, r *http.Request) {
		uid := r.PathValue("tokenUid")
		s.State.mu.RLock()
		token, ok := s.State.Tokens[uid]
		s.State.mu.RUnlock()
		if !ok {
			writeJSON(w, 404, ocpiResponse(nil, 2004, "Token not found"))
			return
		}
		writeJSON(w, 200, okResponse(token))
	})

	mux.HandleFunc("POST /ocpi/sender/2.1.1/tokens/{tokenUid}/authorize", func(w http.ResponseWriter, r *http.Request) {
		uid := r.PathValue("tokenUid")
		s.State.mu.RLock()
		token, ok := s.State.Tokens[uid]
		s.State.mu.RUnlock()
		if !ok {
			writeJSON(w, 404, ocpiResponse(nil, 2004, "Unknown token"))
			return
		}
		time.Sleep(10 * time.Second)
		allowed := "NOT_ALLOWED"
		if v, _ := token["valid"].(bool); v {
			allowed = "ALLOWED"
		}
		writeJSON(w, 200, okResponse(map[string]any{"allowed": allowed}))
	})

	// Locations
	putOrPatchModule := func(module string, merge bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			cc := r.PathValue("countryCode")
			pid := r.PathValue("partyId")
			id := r.PathValue("id")
			key := fmt.Sprintf("%s/%s/%s", cc, pid, id)

			var body map[string]any
			if err := decodeBody(r, &body); err != nil {
				body = map[string]any{}
			}
			s.State.mu.Lock()
			store := s.State.Store(module)
			existing, present := store[key]
			if !present {
				s.State.bumpCount(module, 1)
			}
			var merged map[string]any
			if merge && present {
				merged = existing
				for k, v := range body {
					merged[k] = v
				}
			} else {
				merged = body
				merged["country_code"] = cc
				merged["party_id"] = pid
				merged["id"] = id
			}
			store[key] = merged
			s.State.mu.Unlock()
			s.OnStateChange()
			writeJSON(w, 200, okResponse(nil))
		}
	}

	mux.HandleFunc("PUT /ocpi/receiver/2.1.1/locations/{countryCode}/{partyId}/{id}", putOrPatchModule("locations", false))
	mux.HandleFunc("PATCH /ocpi/receiver/2.1.1/locations/{countryCode}/{partyId}/{id}", putOrPatchModule("locations", true))

	mux.HandleFunc("PUT /ocpi/receiver/2.1.1/sessions/{countryCode}/{partyId}/{id}", putOrPatchModule("sessions", false))
	mux.HandleFunc("PATCH /ocpi/receiver/2.1.1/sessions/{countryCode}/{partyId}/{id}", putOrPatchModule("sessions", true))

	mux.HandleFunc("PUT /ocpi/receiver/2.1.1/tariffs/{countryCode}/{partyId}/{id}", putOrPatchModule("tariffs", false))

	mux.HandleFunc("DELETE /ocpi/receiver/2.1.1/tariffs/{countryCode}/{partyId}/{id}", func(w http.ResponseWriter, r *http.Request) {
		cc := r.PathValue("countryCode")
		pid := r.PathValue("partyId")
		id := r.PathValue("id")
		key := fmt.Sprintf("%s/%s/%s", cc, pid, id)
		s.State.mu.Lock()
		if _, ok := s.State.Tariffs[key]; ok {
			s.State.bumpCount("tariffs", -1)
			delete(s.State.Tariffs, key)
		}
		s.State.mu.Unlock()
		s.OnStateChange()
		writeJSON(w, 200, okResponse(nil))
	})

	// CDRs
	mux.HandleFunc("POST /ocpi/receiver/2.1.1/cdrs", func(w http.ResponseWriter, r *http.Request) {
		var cdr map[string]any
		if err := decodeBody(r, &cdr); err != nil {
			cdr = map[string]any{}
		}
		id, _ := cdr["id"].(string)
		if id == "" {
			id = fmt.Sprintf("cdr-%d", time.Now().UnixNano())
			cdr["id"] = id
		}
		s.State.mu.Lock()
		if _, ok := s.State.CDRs[id]; !ok {
			s.State.bumpCount("cdrs", 1)
		}
		s.State.CDRs[id] = cdr
		s.State.mu.Unlock()
		s.OnStateChange()
		w.Header().Set("Location", fmt.Sprintf("%s/ocpi/receiver/2.1.1/cdrs/%s", s.URL, id))
		writeJSON(w, 201, okResponse(nil))
	})

	mux.HandleFunc("GET /ocpi/receiver/2.1.1/cdrs/{cdrId}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("cdrId")
		s.State.mu.RLock()
		cdr, ok := s.State.CDRs[id]
		s.State.mu.RUnlock()
		if !ok {
			writeJSON(w, 404, ocpiResponse(nil, 2004, "CDR not found"))
			return
		}
		writeJSON(w, 200, okResponse(cdr))
	})

	// Commands
	mux.HandleFunc("POST /ocpi/receiver/2.1.1/commands/{command}/{uid}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, okResponse(nil))
	})

	// Catch-all
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 404, ocpiResponse(nil, 2000, "Endpoint not found: "+r.URL.RequestURI()))
	})

	s.mux = mux
}

func (s *Server) ListenAndServe() error {
	s.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.Port),
		Handler: s.logMiddleware(s.mux),
	}
	return s.httpServer.ListenAndServe()
}
