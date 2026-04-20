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
	PeerVersion     string
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

const (
	VersionV211 = "2.1.1"
	VersionV221 = "2.2.1"
)

// SupportedVersions is the order in which versions are advertised; first is
// preferred during client-side negotiation.
var SupportedVersions = []string{VersionV221, VersionV211}

// ocpiRole maps our internal role to the OCPI 2.2.1 role string.
func ocpiRole(role string) string {
	if role == RoleCPO {
		return "CPO"
	}
	return "EMSP"
}

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

func (s *Server) identity(version string) map[string]any {
	token := "mocked-msp-token"
	name := "Mock MSP"
	if s.Role == RoleCPO {
		token = "mocked-cpo-token"
		name = "Mock CPO"
	}
	if version == VersionV221 {
		return map[string]any{
			"token": token,
			"url":   s.URL + "/ocpi/versions",
			"roles": []map[string]any{
				{
					"role":             ocpiRole(s.Role),
					"business_details": map[string]any{"name": name},
					"party_id":         s.PartyID,
					"country_code":     s.CountryCode,
				},
			},
		}
	}
	return map[string]any{
		"token":            token,
		"url":              s.URL + "/ocpi/versions",
		"business_details": map[string]any{"name": name},
		"party_id":         s.PartyID,
		"country_code":     s.CountryCode,
	}
}

func (s *Server) endpointsList(version string) []map[string]any {
	tag := func(identifier, role, url string) map[string]any {
		e := map[string]any{"identifier": identifier, "url": url}
		if version == VersionV221 {
			e["role"] = role
		}
		return e
	}
	base := s.URL + "/ocpi"
	if s.Role == RoleCPO {
		return []map[string]any{
			tag("credentials", "SENDER", base+"/"+version+"/credentials"),
			tag("locations", "SENDER", base+"/sender/"+version+"/locations"),
			tag("sessions", "SENDER", base+"/sender/"+version+"/sessions"),
			tag("cdrs", "SENDER", base+"/sender/"+version+"/cdrs"),
			tag("tariffs", "SENDER", base+"/sender/"+version+"/tariffs"),
			tag("tokens", "RECEIVER", base+"/receiver/"+version+"/tokens"),
			tag("commands", "RECEIVER", base+"/receiver/"+version+"/commands"),
		}
	}
	return []map[string]any{
		tag("credentials", "SENDER", base+"/"+version+"/credentials"),
		tag("locations", "RECEIVER", base+"/receiver/"+version+"/locations"),
		tag("sessions", "RECEIVER", base+"/receiver/"+version+"/sessions"),
		tag("cdrs", "RECEIVER", base+"/receiver/"+version+"/cdrs"),
		tag("tariffs", "RECEIVER", base+"/receiver/"+version+"/tariffs"),
		tag("tokens", "SENDER", base+"/sender/"+version+"/tokens"),
		tag("commands", "RECEIVER", base+"/receiver/"+version+"/commands"),
	}
}

func (s *Server) routes() {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /ocpi/versions", func(w http.ResponseWriter, r *http.Request) {
		entries := make([]map[string]any, 0, len(SupportedVersions))
		for _, v := range SupportedVersions {
			entries = append(entries, map[string]any{"version": v, "url": s.URL + "/ocpi/" + v})
		}
		writeJSON(w, 200, okResponse(entries))
	})

	for _, v := range SupportedVersions {
		s.registerVersionRoutes(mux, v)
	}

	// Catch-all
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 404, ocpiResponse(nil, 2000, "Endpoint not found: "+r.URL.RequestURI()))
	})

	s.mux = mux
}

func (s *Server) registerVersionRoutes(mux *http.ServeMux, v string) {
	mux.HandleFunc("GET /ocpi/"+v, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, okResponse(map[string]any{
			"version":   v,
			"endpoints": s.endpointsList(v),
		}))
	})

	mux.HandleFunc("GET /ocpi/"+v+"/credentials", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, okResponse(s.identity(v)))
	})

	mux.HandleFunc("PUT /ocpi/"+v+"/credentials", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := decodeBody(r, &body); err != nil || body["token"] == nil {
			writeJSON(w, 400, ocpiResponse(nil, 2001, "Token is required"))
			return
		}
		s.State.mu.Lock()
		s.State.PeerCredentials = body
		s.State.PeerVersion = v
		s.State.mu.Unlock()
		s.OnStateChange()
		writeJSON(w, 200, okResponse(s.identity(v)))
	})

	mux.HandleFunc("DELETE /ocpi/"+v+"/credentials", func(w http.ResponseWriter, r *http.Request) {
		s.State.mu.Lock()
		s.State.PeerCredentials = nil
		s.State.PeerVersion = ""
		s.State.mu.Unlock()
		s.OnStateChange()
		writeJSON(w, 200, okResponse(nil))
	})

	if s.Role == RoleCPO {
		s.registerCPORoutes(mux, v)
	} else {
		s.registerMSPRoutes(mux, v)
	}
}

// asMapSlice normalizes a JSON array value (which may decode as []any or be
// constructed as []map[string]any) to []map[string]any for in-place editing.
func asMapSlice(v any) []map[string]any {
	switch arr := v.(type) {
	case []map[string]any:
		return arr
	case []any:
		out := make([]map[string]any, 0, len(arr))
		for _, it := range arr {
			if m, ok := it.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}

// upsertChild finds an entry in a map-slice where `keyField` equals `id`. If
// found and merge is true, merges body into it; otherwise replaces it. If not
// found, appends body (with keyField set to id).
func upsertChild(children []map[string]any, keyField, id string, body map[string]any, merge bool) []map[string]any {
	for i, c := range children {
		if v, _ := c[keyField].(string); v == id {
			if merge {
				for k, val := range body {
					c[k] = val
				}
				children[i] = c
			} else {
				body[keyField] = id
				children[i] = body
			}
			return children
		}
	}
	body[keyField] = id
	return append(children, body)
}

// findChild returns the entry in a map-slice whose keyField equals id.
func findChild(children []map[string]any, keyField, id string) (map[string]any, bool) {
	for _, c := range children {
		if v, _ := c[keyField].(string); v == id {
			return c, true
		}
	}
	return nil, false
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

// putOrPatchEVSE upserts an EVSE into a location's `evses` array.
func (s *Server) putOrPatchEVSE(merge bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cc := r.PathValue("countryCode")
		pid := r.PathValue("partyId")
		id := r.PathValue("id")
		evseUID := r.PathValue("evseUid")
		key := fmt.Sprintf("%s/%s/%s", cc, pid, id)

		var body map[string]any
		if err := decodeBody(r, &body); err != nil {
			body = map[string]any{}
		}

		s.State.mu.Lock()
		loc, ok := s.State.Locations[key]
		if !ok {
			s.State.mu.Unlock()
			writeJSON(w, 404, ocpiResponse(nil, 2004, "Location not found"))
			return
		}
		evses := asMapSlice(loc["evses"])
		evses = upsertChild(evses, "uid", evseUID, body, merge)
		loc["evses"] = evses
		loc["last_updated"] = nowISO()
		s.State.Locations[key] = loc
		s.State.mu.Unlock()
		s.OnStateChange()
		writeJSON(w, 200, okResponse(nil))
	}
}

// putOrPatchConnector upserts a Connector into an EVSE's `connectors` array.
func (s *Server) putOrPatchConnector(merge bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cc := r.PathValue("countryCode")
		pid := r.PathValue("partyId")
		id := r.PathValue("id")
		evseUID := r.PathValue("evseUid")
		connectorID := r.PathValue("connectorId")
		key := fmt.Sprintf("%s/%s/%s", cc, pid, id)

		var body map[string]any
		if err := decodeBody(r, &body); err != nil {
			body = map[string]any{}
		}

		s.State.mu.Lock()
		loc, ok := s.State.Locations[key]
		if !ok {
			s.State.mu.Unlock()
			writeJSON(w, 404, ocpiResponse(nil, 2004, "Location not found"))
			return
		}
		evses := asMapSlice(loc["evses"])
		evse, ok := findChild(evses, "uid", evseUID)
		if !ok {
			s.State.mu.Unlock()
			writeJSON(w, 404, ocpiResponse(nil, 2004, "EVSE not found"))
			return
		}
		connectors := asMapSlice(evse["connectors"])
		connectors = upsertChild(connectors, "id", connectorID, body, merge)
		evse["connectors"] = connectors
		evse["last_updated"] = nowISO()
		loc["evses"] = evses
		loc["last_updated"] = nowISO()
		s.State.Locations[key] = loc
		s.State.mu.Unlock()
		s.OnStateChange()
		writeJSON(w, 200, okResponse(nil))
	}
}

func (s *Server) registerMSPRoutes(mux *http.ServeMux, v string) {
	sender := "/ocpi/sender/" + v
	receiver := "/ocpi/receiver/" + v

	// Tokens (sender)
	mux.HandleFunc("GET "+sender+"/tokens", func(w http.ResponseWriter, r *http.Request) {
		s.State.mu.RLock()
		list := make([]map[string]any, 0, len(s.State.Tokens))
		for _, t := range s.State.Tokens {
			list = append(list, t)
		}
		s.State.mu.RUnlock()
		writeJSON(w, 200, okResponse(list))
	})

	mux.HandleFunc("GET "+sender+"/tokens/{countryCode}/{partyId}/{tokenUid}", func(w http.ResponseWriter, r *http.Request) {
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

	mux.HandleFunc("POST "+sender+"/tokens/{tokenUid}/authorize", func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("PUT "+receiver+"/locations/{countryCode}/{partyId}/{id}", s.putOrPatchModule("locations", "id", false))
	mux.HandleFunc("PATCH "+receiver+"/locations/{countryCode}/{partyId}/{id}", s.putOrPatchModule("locations", "id", true))

	// EVSE sub-object (both versions)
	mux.HandleFunc("PUT "+receiver+"/locations/{countryCode}/{partyId}/{id}/{evseUid}", s.putOrPatchEVSE(false))
	mux.HandleFunc("PATCH "+receiver+"/locations/{countryCode}/{partyId}/{id}/{evseUid}", s.putOrPatchEVSE(true))

	// Connector sub-object (both versions)
	mux.HandleFunc("PUT "+receiver+"/locations/{countryCode}/{partyId}/{id}/{evseUid}/{connectorId}", s.putOrPatchConnector(false))
	mux.HandleFunc("PATCH "+receiver+"/locations/{countryCode}/{partyId}/{id}/{evseUid}/{connectorId}", s.putOrPatchConnector(true))

	mux.HandleFunc("PUT "+receiver+"/sessions/{countryCode}/{partyId}/{id}", s.putOrPatchModule("sessions", "id", false))
	mux.HandleFunc("PATCH "+receiver+"/sessions/{countryCode}/{partyId}/{id}", s.putOrPatchModule("sessions", "id", true))

	mux.HandleFunc("PUT "+receiver+"/tariffs/{countryCode}/{partyId}/{id}", s.putOrPatchModule("tariffs", "id", false))

	mux.HandleFunc("DELETE "+receiver+"/tariffs/{countryCode}/{partyId}/{id}", func(w http.ResponseWriter, r *http.Request) {
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
	cdrLocation := s.URL + receiver + "/cdrs"
	mux.HandleFunc("POST "+receiver+"/cdrs", func(w http.ResponseWriter, r *http.Request) {
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
		w.Header().Set("Location", cdrLocation+"/"+id)
		writeJSON(w, 201, okResponse(nil))
	})

	mux.HandleFunc("GET "+receiver+"/cdrs/{cdrId}", func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("POST "+receiver+"/commands/{command}/{uid}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, okResponse(nil))
	})
}

func (s *Server) registerCPORoutes(mux *http.ServeMux, v string) {
	sender := "/ocpi/sender/" + v
	receiver := "/ocpi/receiver/" + v

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

	mux.HandleFunc("GET "+sender+"/locations", listSender("locations"))
	mux.HandleFunc("GET "+sender+"/locations/{countryCode}/{partyId}/{id}", getSenderByKey("locations"))

	// EVSE sub-object GET
	mux.HandleFunc("GET "+sender+"/locations/{countryCode}/{partyId}/{id}/{evseUid}", func(w http.ResponseWriter, r *http.Request) {
		cc := r.PathValue("countryCode")
		pid := r.PathValue("partyId")
		id := r.PathValue("id")
		evseUID := r.PathValue("evseUid")
		key := fmt.Sprintf("%s/%s/%s", cc, pid, id)
		s.State.mu.RLock()
		loc, ok := s.State.Locations[key]
		s.State.mu.RUnlock()
		if !ok {
			writeJSON(w, 404, ocpiResponse(nil, 2004, "Location not found"))
			return
		}
		evse, ok := findChild(asMapSlice(loc["evses"]), "uid", evseUID)
		if !ok {
			writeJSON(w, 404, ocpiResponse(nil, 2004, "EVSE not found"))
			return
		}
		writeJSON(w, 200, okResponse(evse))
	})

	// Connector sub-object GET
	mux.HandleFunc("GET "+sender+"/locations/{countryCode}/{partyId}/{id}/{evseUid}/{connectorId}", func(w http.ResponseWriter, r *http.Request) {
		cc := r.PathValue("countryCode")
		pid := r.PathValue("partyId")
		id := r.PathValue("id")
		evseUID := r.PathValue("evseUid")
		connectorID := r.PathValue("connectorId")
		key := fmt.Sprintf("%s/%s/%s", cc, pid, id)
		s.State.mu.RLock()
		loc, ok := s.State.Locations[key]
		s.State.mu.RUnlock()
		if !ok {
			writeJSON(w, 404, ocpiResponse(nil, 2004, "Location not found"))
			return
		}
		evse, ok := findChild(asMapSlice(loc["evses"]), "uid", evseUID)
		if !ok {
			writeJSON(w, 404, ocpiResponse(nil, 2004, "EVSE not found"))
			return
		}
		conn, ok := findChild(asMapSlice(evse["connectors"]), "id", connectorID)
		if !ok {
			writeJSON(w, 404, ocpiResponse(nil, 2004, "Connector not found"))
			return
		}
		writeJSON(w, 200, okResponse(conn))
	})

	mux.HandleFunc("GET "+sender+"/sessions", listSender("sessions"))
	mux.HandleFunc("GET "+sender+"/cdrs", listSender("cdrs"))
	mux.HandleFunc("GET "+sender+"/tariffs", listSender("tariffs"))

	// ChargingPreferences sub-object (2.2.1 only) — MSP pushes preferences for a session to CPO.
	if v == VersionV221 {
		mux.HandleFunc("PUT "+sender+"/sessions/{countryCode}/{partyId}/{sessionId}/charging_preferences", func(w http.ResponseWriter, r *http.Request) {
			cc := r.PathValue("countryCode")
			pid := r.PathValue("partyId")
			sid := r.PathValue("sessionId")
			key := fmt.Sprintf("%s/%s/%s", cc, pid, sid)

			var prefs map[string]any
			if err := decodeBody(r, &prefs); err != nil {
				prefs = map[string]any{}
			}

			s.State.mu.Lock()
			sess, ok := s.State.Sessions[key]
			if !ok {
				s.State.mu.Unlock()
				writeJSON(w, 404, ocpiResponse(nil, 2004, "Session not found"))
				return
			}
			sess["charging_preferences"] = prefs
			sess["last_updated"] = nowISO()
			s.State.Sessions[key] = sess
			s.State.mu.Unlock()
			s.OnStateChange()

			profileType, _ := prefs["profile_type"].(string)
			if profileType == "" {
				profileType = "REGULAR"
			}
			writeJSON(w, 200, okResponse(map[string]any{"result": "ACCEPTED", "profile_type": profileType}))
		})
	}

	// Receiver: tokens (MSP pushes these)
	mux.HandleFunc("PUT "+receiver+"/tokens/{countryCode}/{partyId}/{tokenUid}", s.putOrPatchModule("tokens", "tokenUid", false))
	mux.HandleFunc("PATCH "+receiver+"/tokens/{countryCode}/{partyId}/{tokenUid}", s.putOrPatchModule("tokens", "tokenUid", true))

	// Receiver: commands (MSP sends commands to CPO)
	mux.HandleFunc("POST "+receiver+"/commands/{command}", func(w http.ResponseWriter, r *http.Request) {
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
