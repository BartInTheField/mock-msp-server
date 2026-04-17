package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var moduleEndpointIDs = map[string]string{
	"locations": "locations",
	"sessions":  "sessions",
	"cdrs":      "cdrs",
	"tariffs":   "tariffs",
	"tokens":    "tokens",
}

func (s *Server) discoverEndpoint(module string) (string, string, error) {
	s.State.mu.RLock()
	creds := s.State.PeerCredentials
	s.State.mu.RUnlock()

	peerLabel := "CPO"
	if s.Role == RoleCPO {
		peerLabel = "MSP"
	}
	if creds == nil {
		return "", "", fmt.Errorf("no %s credentials available — register first", peerLabel)
	}
	token, _ := creds["token"].(string)
	baseURL, _ := creds["url"].(string)
	if token == "" || baseURL == "" {
		return "", "", fmt.Errorf("no %s credentials available — register first", peerLabel)
	}

	versionsBody, err := s.authedGetJSON(baseURL, token)
	if err != nil {
		return "", "", err
	}
	versions, _ := versionsBody["data"].([]any)
	var v2URL string
	for _, v := range versions {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if m["version"] == "2.1.1" {
			v2URL, _ = m["url"].(string)
			break
		}
	}
	if v2URL == "" {
		return "", "", fmt.Errorf("%s does not support OCPI 2.1.1", peerLabel)
	}

	detailsBody, err := s.authedGetJSON(v2URL, token)
	if err != nil {
		return "", "", err
	}
	data, _ := detailsBody["data"].(map[string]any)
	endpoints, _ := data["endpoints"].([]any)
	want := moduleEndpointIDs[module]
	for _, e := range endpoints {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		if m["identifier"] == want {
			u, _ := m["url"].(string)
			return u, token, nil
		}
	}
	return "", "", fmt.Errorf("%s has no %s endpoint", peerLabel, module)
}

// PullableModules returns the OCPI modules this role can pull from its peer.
func (s *Server) PullableModules() []string {
	if s.Role == RoleCPO {
		return []string{"tokens"}
	}
	return []string{"locations", "sessions", "cdrs", "tariffs", "tokens"}
}

func (s *Server) canPull(module string) bool {
	for _, m := range s.PullableModules() {
		if m == module {
			return true
		}
	}
	return false
}

// Register performs the OCPI credentials handshake against the given peer base
// URL. It discovers the peer's versions + credentials endpoint, PUTs our own
// identity, and stores the real peer credentials returned. The peer URL can be
// the versions URL directly (containing "/ocpi/") or just a base (e.g.
// "http://localhost:3011"), in which case "/ocpi/versions" is appended.
func (s *Server) Register(peerURL string) string {
	if peerURL == "" {
		return "Failed: no peer URL"
	}
	// The initial token is the well-known mocked token of the opposite role.
	initialToken := "mocked-cpo-token"
	peerLabel := "CPO"
	if s.Role == RoleCPO {
		initialToken = "mocked-msp-token"
		peerLabel = "MSP"
	}

	versionsURL := strings.TrimRight(peerURL, "/")
	if !strings.Contains(versionsURL, "/ocpi/") {
		versionsURL += "/ocpi/versions"
	}

	body, err := s.authedGetJSON(versionsURL, initialToken)
	if err != nil {
		return "Register failed: " + err.Error()
	}
	versions, _ := body["data"].([]any)
	var v2URL string
	for _, v := range versions {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if m["version"] == "2.1.1" {
			v2URL, _ = m["url"].(string)
			break
		}
	}
	if v2URL == "" {
		return fmt.Sprintf("Register failed: %s does not support OCPI 2.1.1", peerLabel)
	}

	details, err := s.authedGetJSON(v2URL, initialToken)
	if err != nil {
		return "Register failed: " + err.Error()
	}
	data, _ := details["data"].(map[string]any)
	endpoints, _ := data["endpoints"].([]any)
	var credURL string
	for _, e := range endpoints {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		if m["identifier"] == "credentials" {
			credURL, _ = m["url"].(string)
			break
		}
	}
	if credURL == "" {
		return fmt.Sprintf("Register failed: %s has no credentials endpoint", peerLabel)
	}

	payload, err := json.Marshal(s.identity())
	if err != nil {
		return "Register failed: " + err.Error()
	}
	s.OnLog(LogEntry{Timestamp: nowISO(), Method: "OUT", URL: "PUT " + credURL})
	req, err := http.NewRequest("PUT", credURL, bytes.NewReader(payload))
	if err != nil {
		return "Register failed: " + err.Error()
	}
	req.Header.Set("Authorization", "Token "+initialToken)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return "Register failed: " + err.Error()
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return "Register failed: " + err.Error()
	}
	if res.StatusCode >= 300 {
		return fmt.Sprintf("Register failed: HTTP %d — %s", res.StatusCode, strings.TrimSpace(string(raw)))
	}
	var respBody map[string]any
	if err := json.Unmarshal(raw, &respBody); err != nil {
		return "Register failed: invalid JSON response"
	}
	peerCreds, _ := respBody["data"].(map[string]any)
	if peerCreds == nil {
		return "Register failed: no credentials in response"
	}

	s.State.mu.Lock()
	s.State.PeerCredentials = peerCreds
	s.State.mu.Unlock()
	s.OnStateChange()

	name := ""
	if bd, ok := peerCreds["business_details"].(map[string]any); ok {
		name, _ = bd["name"].(string)
	}
	if name == "" {
		name = peerLabel
	}
	return "Registered with " + name
}

func (s *Server) authedGetJSON(url, token string) (map[string]any, error) {
	s.OnLog(LogEntry{Timestamp: nowISO(), Method: "OUT", URL: url})
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Token "+token)
	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	return body, nil
}

func (s *Server) storeItems(module string, items []any) {
	s.State.mu.Lock()
	defer s.State.mu.Unlock()
	store := s.State.Store(module)
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		var key string
		switch module {
		case "cdrs":
			if id, ok := m["id"].(string); ok {
				key = id
			} else {
				key = fmt.Sprintf("cdr-%d", time.Now().UnixNano())
			}
		case "tokens":
			if uid, ok := m["uid"].(string); ok {
				key = uid
			} else if id, ok := m["id"].(string); ok {
				key = id
			} else {
				key = fmt.Sprintf("token-%d", time.Now().UnixNano())
			}
		default:
			cc, _ := m["country_code"].(string)
			pid, _ := m["party_id"].(string)
			id, _ := m["id"].(string)
			key = fmt.Sprintf("%s/%s/%s", cc, pid, id)
		}
		if _, exists := store[key]; !exists {
			s.State.bumpCount(module, 1)
		}
		store[key] = m
	}
}

func (s *Server) PullModule(module string) string {
	if !s.canPull(module) {
		return fmt.Sprintf("Pulling %s is not available in %s mode", module, s.Role)
	}
	endpoint, token, err := s.discoverEndpoint(module)
	if err != nil {
		return "Failed: " + err.Error()
	}
	body, err := s.authedGetJSON(endpoint, token)
	if err != nil {
		return "Failed: " + err.Error()
	}
	items, ok := body["data"].([]any)
	if !ok {
		return fmt.Sprintf("No %s in response", module)
	}
	s.storeItems(module, items)
	s.OnStateChange()
	peerLabel := "CPO"
	if s.Role == RoleCPO {
		peerLabel = "MSP"
	}
	return fmt.Sprintf("Pulled %d %s from %s", len(items), module, peerLabel)
}
