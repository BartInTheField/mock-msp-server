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
	mu              sync.RWMutex
	PeerCredentials map[string]any
	Locations       map[string]map[string]any
	Sessions        map[string]map[string]any
	CDRs            map[string]map[string]any
	Tariffs         map[string]map[string]any
	Tokens          map[string]map[string]any
	Counts          ModuleCounts
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

const (
	RoleMSP = "msp"
	RoleCPO = "cpo"
)

// OwnsModule reports whether this role is the OCPI sender for the given
// module — i.e. items in that module belong to this party (asset). The
// opposite case is "received" (this party is the receiver).
func OwnsModule(role, module string) bool {
	if role == RoleCPO {
		switch module {
		case "locations", "sessions", "cdrs", "tariffs":
			return true
		}
		return false
	}
	// MSP
	return module == "tokens"
}

type Server struct {
	Role          string
	PartyID       string
	CountryCode   string
	URL           string
	Port          int
	State         *ServerState
	OnLog         OnLog
	OnStateChange OnStateChange
	mux           *http.ServeMux
	httpServer    *http.Server
}

// DefaultPartyID returns the default OCPI party_id for a role.
func DefaultPartyID(role string) string {
	if role == RoleCPO {
		return "CPO"
	}
	return "MSP"
}

func NewServer(role, partyID, countryCode, url string, port int, onLog OnLog, onStateChange OnStateChange) *Server {
	state := &ServerState{
		Locations: map[string]map[string]any{},
		Sessions:  map[string]map[string]any{},
		CDRs:      map[string]map[string]any{},
		Tariffs:   map[string]map[string]any{},
		Tokens:    map[string]map[string]any{},
	}

	s := &Server{
		Role:          role,
		PartyID:       partyID,
		CountryCode:   countryCode,
		URL:           url,
		Port:          port,
		State:         state,
		OnLog:         onLog,
		OnStateChange: onStateChange,
	}

	switch role {
	case RoleMSP:
		seedMSPTokens(state, countryCode, partyID)
	case RoleCPO:
		seedCPOData(state, countryCode, partyID)
	}

	s.routes()
	return s
}

func seedMSPTokens(state *ServerState, cc, pid string) {
	prefix := cc + "-" + pid
	state.Tokens["valid-token-1"] = map[string]any{
		"uid":           "valid-token-1",
		"type":          "RFID",
		"auth_id":       prefix + "-valid-token-1",
		"visual_number": prefix + "-000001",
		"issuer":        "Mock MSP",
		"valid":         true,
		"whitelist":     "ALLOWED",
		"last_updated":  "2025-01-01T00:00:00Z",
	}
	state.Tokens["valid-token-2"] = map[string]any{
		"uid":           "valid-token-2",
		"type":          "RFID",
		"auth_id":       prefix + "-valid-token-2",
		"visual_number": prefix + "-000002",
		"issuer":        "Mock MSP",
		"valid":         true,
		"whitelist":     "ALLOWED",
		"last_updated":  "2025-01-01T00:00:00Z",
	}
	state.Counts.Tokens = 2
}

func seedCPOData(state *ServerState, cc, pid string) {
	location := map[string]any{
		"id":           "LOC001",
		"country_code": cc,
		"party_id":     pid,
		"type":         "ON_STREET",
		"name":         "Mock CPO Station 1",
		"address":      "Mainstreet 1",
		"city":         "Amsterdam",
		"postal_code":  "1011AA",
		"country":      "NLD",
		"coordinates": map[string]any{
			"latitude":  "52.370216",
			"longitude": "4.895168",
		},
		"evses": []map[string]any{
			{
				"uid":            "EVSE001",
				"evse_id":        cc + "*" + pid + "*E000001*1",
				"status":         "AVAILABLE",
				"capabilities":   []string{"RFID_READER"},
				"physical_reference": "1",
				"connectors": []map[string]any{
					{
						"id":              "1",
						"standard":        "IEC_62196_T2",
						"format":          "SOCKET",
						"power_type":      "AC_3_PHASE",
						"voltage":         230,
						"amperage":        32,
						"tariff_id":       "TARIFF001",
						"last_updated":    "2025-01-01T00:00:00Z",
					},
				},
				"last_updated": "2025-01-01T00:00:00Z",
			},
		},
		"operator": map[string]any{
			"name": "Mock CPO",
		},
		"last_updated": "2025-01-01T00:00:00Z",
	}
	state.Locations[cc+"/"+pid+"/LOC001"] = location
	state.Counts.Locations = 1

	tariff := map[string]any{
		"id":           "TARIFF001",
		"country_code": cc,
		"party_id":     pid,
		"currency":     "EUR",
		"elements": []map[string]any{
			{
				"price_components": []map[string]any{
					{
						"type":     "ENERGY",
						"price":    0.25,
						"step_size": 1,
					},
				},
			},
		},
		"last_updated": "2025-01-01T00:00:00Z",
	}
	state.Tariffs[cc+"/"+pid+"/TARIFF001"] = tariff
	state.Counts.Tariffs = 1
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

func (s *Server) identity() map[string]any {
	token := "mocked-msp-token"
	name := "Mock MSP"
	if s.Role == RoleCPO {
		token = "mocked-cpo-token"
		name = "Mock CPO"
	}
	return map[string]any{
		"token":            token,
		"url":              s.URL + "/ocpi/versions",
		"business_details": map[string]any{"name": name},
		"party_id":         s.PartyID,
		"country_code":     s.CountryCode,
	}
}

func (s *Server) endpointsList() []map[string]any {
	if s.Role == RoleCPO {
		return []map[string]any{
			{"identifier": "credentials", "url": s.URL + "/ocpi/2.1.1/credentials"},
			{"identifier": "locations", "url": s.URL + "/ocpi/sender/2.1.1/locations"},
			{"identifier": "sessions", "url": s.URL + "/ocpi/sender/2.1.1/sessions"},
			{"identifier": "cdrs", "url": s.URL + "/ocpi/sender/2.1.1/cdrs"},
			{"identifier": "tariffs", "url": s.URL + "/ocpi/sender/2.1.1/tariffs"},
			{"identifier": "tokens", "url": s.URL + "/ocpi/receiver/2.1.1/tokens"},
			{"identifier": "commands", "url": s.URL + "/ocpi/receiver/2.1.1/commands"},
		}
	}
	return []map[string]any{
		{"identifier": "credentials", "url": s.URL + "/ocpi/2.1.1/credentials"},
		{"identifier": "locations", "url": s.URL + "/ocpi/receiver/2.1.1/locations"},
		{"identifier": "sessions", "url": s.URL + "/ocpi/receiver/2.1.1/sessions"},
		{"identifier": "cdrs", "url": s.URL + "/ocpi/receiver/2.1.1/cdrs"},
		{"identifier": "tariffs", "url": s.URL + "/ocpi/receiver/2.1.1/tariffs"},
		{"identifier": "tokens", "url": s.URL + "/ocpi/sender/2.1.1/tokens"},
		{"identifier": "commands", "url": s.URL + "/ocpi/receiver/2.1.1/commands"},
	}
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
			"version":   "2.1.1",
			"endpoints": s.endpointsList(),
		}))
	})

	// Credentials
	mux.HandleFunc("GET /ocpi/2.1.1/credentials", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, okResponse(s.identity()))
	})

	mux.HandleFunc("PUT /ocpi/2.1.1/credentials", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := decodeBody(r, &body); err != nil || body["token"] == nil {
			writeJSON(w, 400, ocpiResponse(nil, 2001, "Token is required"))
			return
		}
		s.State.mu.Lock()
		s.State.PeerCredentials = body
		s.State.mu.Unlock()
		s.OnStateChange()
		writeJSON(w, 200, okResponse(s.identity()))
	})

	mux.HandleFunc("DELETE /ocpi/2.1.1/credentials", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, okResponse(nil))
	})

	if s.Role == RoleCPO {
		s.registerCPORoutes(mux)
	} else {
		s.registerMSPRoutes(mux)
	}

	// Catch-all
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 404, ocpiResponse(nil, 2000, "Endpoint not found: "+r.URL.RequestURI()))
	})

	s.mux = mux
}

// putOrPatchModule returns a handler that stores/updates an item keyed by
// {countryCode}/{partyId}/{idParam}. Used for locations, sessions, tariffs, tokens.
func (s *Server) putOrPatchModule(module, idParam string, merge bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cc := r.PathValue("countryCode")
		pid := r.PathValue("partyId")
		id := r.PathValue(idParam)
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
			if idParam == "tokenUid" {
				merged["uid"] = id
			} else {
				merged["id"] = id
			}
		}
		store[key] = merged
		s.State.mu.Unlock()
		s.OnStateChange()
		writeJSON(w, 200, okResponse(nil))
	}
}

func (s *Server) registerMSPRoutes(mux *http.ServeMux) {
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

	// Locations / Sessions / Tariffs receivers
	mux.HandleFunc("PUT /ocpi/receiver/2.1.1/locations/{countryCode}/{partyId}/{id}", s.putOrPatchModule("locations", "id", false))
	mux.HandleFunc("PATCH /ocpi/receiver/2.1.1/locations/{countryCode}/{partyId}/{id}", s.putOrPatchModule("locations", "id", true))

	mux.HandleFunc("PUT /ocpi/receiver/2.1.1/sessions/{countryCode}/{partyId}/{id}", s.putOrPatchModule("sessions", "id", false))
	mux.HandleFunc("PATCH /ocpi/receiver/2.1.1/sessions/{countryCode}/{partyId}/{id}", s.putOrPatchModule("sessions", "id", true))

	mux.HandleFunc("PUT /ocpi/receiver/2.1.1/tariffs/{countryCode}/{partyId}/{id}", s.putOrPatchModule("tariffs", "id", false))

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

	// Commands (async result callback from CPO)
	mux.HandleFunc("POST /ocpi/receiver/2.1.1/commands/{command}/{uid}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, okResponse(nil))
	})
}

func (s *Server) registerCPORoutes(mux *http.ServeMux) {
	// Sender: locations / sessions / cdrs / tariffs (MSP pulls these)
	listSender := func(module string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			s.State.mu.RLock()
			store := s.State.Store(module)
			list := make([]map[string]any, 0, len(store))
			for _, v := range store {
				list = append(list, v)
			}
			s.State.mu.RUnlock()
			writeJSON(w, 200, okResponse(list))
		}
	}
	getSenderByKey := func(module string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			cc := r.PathValue("countryCode")
			pid := r.PathValue("partyId")
			id := r.PathValue("id")
			key := fmt.Sprintf("%s/%s/%s", cc, pid, id)
			s.State.mu.RLock()
			obj, ok := s.State.Store(module)[key]
			s.State.mu.RUnlock()
			if !ok {
				writeJSON(w, 404, ocpiResponse(nil, 2004, module+" not found"))
				return
			}
			writeJSON(w, 200, okResponse(obj))
		}
	}

	mux.HandleFunc("GET /ocpi/sender/2.1.1/locations", listSender("locations"))
	mux.HandleFunc("GET /ocpi/sender/2.1.1/locations/{countryCode}/{partyId}/{id}", getSenderByKey("locations"))

	mux.HandleFunc("GET /ocpi/sender/2.1.1/sessions", listSender("sessions"))
	mux.HandleFunc("GET /ocpi/sender/2.1.1/cdrs", listSender("cdrs"))
	mux.HandleFunc("GET /ocpi/sender/2.1.1/tariffs", listSender("tariffs"))

	// Receiver: tokens (MSP pushes these)
	mux.HandleFunc("PUT /ocpi/receiver/2.1.1/tokens/{countryCode}/{partyId}/{tokenUid}", s.putOrPatchModule("tokens", "tokenUid", false))
	mux.HandleFunc("PATCH /ocpi/receiver/2.1.1/tokens/{countryCode}/{partyId}/{tokenUid}", s.putOrPatchModule("tokens", "tokenUid", true))

	// Receiver: commands (MSP sends commands to CPO)
	mux.HandleFunc("POST /ocpi/receiver/2.1.1/commands/{command}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, okResponse(map[string]any{"result": "ACCEPTED"}))
	})
}

func (s *Server) ListenAndServe() error {
	s.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.Port),
		Handler: s.logMiddleware(s.mux),
	}
	return s.httpServer.ListenAndServe()
}
