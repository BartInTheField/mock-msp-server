package main

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func startServer(t *testing.T, role string) *Server {
	t.Helper()
	// Grab a free port
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	url := "http://localhost:" + strconv.Itoa(port)
	srv := NewServer(role, DefaultPartyID(role), "NL", url, port, func(LogEntry) {}, func() {})
	go func() { _ = srv.ListenAndServe() }()

	// Wait for port to be bound (max 2s)
	for i := 0; i < 40; i++ {
		c, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(port))
		if err == nil {
			c.Close()
			return srv
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("server on port %d did not start", port)
	return nil
}

func TestRegisterHandshake(t *testing.T) {
	cpo := startServer(t, RoleCPO)
	msp := startServer(t, RoleMSP)

	result := msp.Register(cpo.URL)
	if !strings.HasPrefix(result, "Registered with") {
		t.Fatalf("msp.Register: %s", result)
	}

	// MSP should now have CPO credentials
	msp.State.mu.RLock()
	creds := msp.State.PeerCredentials
	mspPeerVersion := msp.State.PeerVersion
	msp.State.mu.RUnlock()
	if creds == nil {
		t.Fatal("MSP has no PeerCredentials after Register")
	}
	if name := peerName(creds); name != "Mock CPO" {
		t.Errorf("expected CPO business_details.name=Mock CPO, got %q", name)
	}
	if mspPeerVersion != VersionV221 {
		t.Errorf("expected negotiated version 2.2.1, got %q", mspPeerVersion)
	}

	// CPO should now have MSP credentials stored (the PUT side of the handshake)
	cpo.State.mu.RLock()
	cpoPeer := cpo.State.PeerCredentials
	cpoPeerVersion := cpo.State.PeerVersion
	cpo.State.mu.RUnlock()
	if cpoPeer == nil {
		t.Fatal("CPO has no PeerCredentials after receiving PUT")
	}
	if name := peerName(cpoPeer); name != "Mock MSP" {
		t.Errorf("expected MSP business_details.name=Mock MSP, got %q", name)
	}
	if cpoPeerVersion != VersionV221 {
		t.Errorf("expected CPO to record negotiated version 2.2.1, got %q", cpoPeerVersion)
	}

	// Now MSP can pull locations from CPO
	pull := msp.PullModule("locations")
	if !strings.Contains(pull, "Pulled 1 locations") {
		t.Errorf("expected 'Pulled 1 locations', got %q", pull)
	}
	if msp.State.Counts.Locations != 1 {
		t.Errorf("expected 1 location stored on MSP, got %d", msp.State.Counts.Locations)
	}

	// And CPO can pull tokens from MSP
	pull = cpo.PullModule("tokens")
	if !strings.Contains(pull, "Pulled 2 tokens") {
		t.Errorf("expected 'Pulled 2 tokens', got %q", pull)
	}
}

func TestRegisterCPOToMSP(t *testing.T) {
	// Opposite direction: CPO registers with MSP
	msp := startServer(t, RoleMSP)
	cpo := startServer(t, RoleCPO)

	result := cpo.Register(msp.URL)
	if !strings.HasPrefix(result, "Registered with") {
		t.Fatalf("cpo.Register: %s", result)
	}

	cpo.State.mu.RLock()
	creds := cpo.State.PeerCredentials
	cpo.State.mu.RUnlock()
	if creds == nil {
		t.Fatal("CPO has no PeerCredentials after Register")
	}
	if name := peerName(creds); name != "Mock MSP" {
		t.Errorf("expected MSP business_details.name=Mock MSP, got %q", name)
	}
}

func getJSON(t *testing.T, url string) map[string]any {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode %s: %v (body=%s)", url, err, string(raw))
	}
	return m
}

func TestVersionsEndpointLists211And221(t *testing.T) {
	srv := startServer(t, RoleCPO)
	body := getJSON(t, srv.URL+"/ocpi/versions")
	list, _ := body["data"].([]any)
	if len(list) != 2 {
		t.Fatalf("expected 2 versions, got %d: %v", len(list), list)
	}
	seen := map[string]bool{}
	for _, v := range list {
		m, _ := v.(map[string]any)
		ver, _ := m["version"].(string)
		url, _ := m["url"].(string)
		seen[ver] = true
		if !strings.HasSuffix(url, "/ocpi/"+ver) {
			t.Errorf("version %s has unexpected url %q", ver, url)
		}
	}
	if !seen[VersionV211] || !seen[VersionV221] {
		t.Errorf("expected both 2.1.1 and 2.2.1, got %v", seen)
	}
}

func TestV221EndpointsListCarriesRole(t *testing.T) {
	cpo := startServer(t, RoleCPO)
	body := getJSON(t, cpo.URL+"/ocpi/"+VersionV221)
	data, _ := body["data"].(map[string]any)
	endpoints, _ := data["endpoints"].([]any)
	if len(endpoints) == 0 {
		t.Fatalf("no endpoints returned")
	}
	roles := map[string]string{}
	for _, e := range endpoints {
		m, _ := e.(map[string]any)
		id, _ := m["identifier"].(string)
		role, _ := m["role"].(string)
		if role == "" {
			t.Errorf("endpoint %q missing role field in 2.2.1 response", id)
		}
		roles[id] = role
	}
	if roles["locations"] != "SENDER" {
		t.Errorf("CPO should be SENDER for locations, got %q", roles["locations"])
	}
	if roles["tokens"] != "RECEIVER" {
		t.Errorf("CPO should be RECEIVER for tokens, got %q", roles["tokens"])
	}

	// And 2.1.1 must NOT carry the role field.
	body211 := getJSON(t, cpo.URL+"/ocpi/"+VersionV211)
	data211, _ := body211["data"].(map[string]any)
	endpoints211, _ := data211["endpoints"].([]any)
	for _, e := range endpoints211 {
		m, _ := e.(map[string]any)
		if _, ok := m["role"]; ok {
			t.Errorf("2.1.1 endpoints must not carry role field, got %v", m)
		}
	}
}

func TestV221CredentialsShape(t *testing.T) {
	msp := startServer(t, RoleMSP)
	body := getJSON(t, msp.URL+"/ocpi/"+VersionV221+"/credentials")
	data, _ := body["data"].(map[string]any)
	if _, ok := data["business_details"]; ok {
		t.Errorf("2.2.1 credentials must not expose flat business_details")
	}
	roles, ok := data["roles"].([]any)
	if !ok || len(roles) == 0 {
		t.Fatalf("expected roles[] in 2.2.1 credentials, got %v", data)
	}
	r0, _ := roles[0].(map[string]any)
	if role, _ := r0["role"].(string); role != "EMSP" {
		t.Errorf("MSP should report role=EMSP in 2.2.1, got %q", role)
	}
	bd, _ := r0["business_details"].(map[string]any)
	if name, _ := bd["name"].(string); name != "Mock MSP" {
		t.Errorf("expected business_details.name=Mock MSP, got %q", name)
	}

	// And the 2.1.1 credentials must remain flat.
	body211 := getJSON(t, msp.URL+"/ocpi/"+VersionV211+"/credentials")
	data211, _ := body211["data"].(map[string]any)
	if _, ok := data211["roles"]; ok {
		t.Errorf("2.1.1 credentials must not carry roles[] — got %v", data211)
	}
	bd211, _ := data211["business_details"].(map[string]any)
	if name, _ := bd211["name"].(string); name != "Mock MSP" {
		t.Errorf("2.1.1 business_details.name expected Mock MSP, got %q", name)
	}
}
