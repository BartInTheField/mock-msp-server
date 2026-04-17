package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	creds := s.State.CPOCredentials
	s.State.mu.RUnlock()

	if creds == nil {
		return "", "", fmt.Errorf("no CPO credentials available — register first")
	}
	token, _ := creds["token"].(string)
	baseURL, _ := creds["url"].(string)
	if token == "" || baseURL == "" {
		return "", "", fmt.Errorf("no CPO credentials available — register first")
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
		return "", "", fmt.Errorf("CPO does not support OCPI 2.1.1")
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
	return "", "", fmt.Errorf("CPO has no %s endpoint", module)
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
	return fmt.Sprintf("Pulled %d %s from CPO", len(items), module)
}
