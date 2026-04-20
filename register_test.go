package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func doJSON(t *testing.T, method, url string, body any) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	m := map[string]any{}
	if len(bytes.TrimSpace(raw)) > 0 {
		_ = json.Unmarshal(raw, &m)
	}
	return res.StatusCode, m
}

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

func TestLocationEVSESubObjectPutPatch(t *testing.T) {
	msp := startServer(t, RoleMSP)

	// First create the parent location.
	locURL := msp.URL + "/ocpi/receiver/" + VersionV221 + "/locations/NL/CPO/LOC9"
	status, _ := doJSON(t, "PUT", locURL, map[string]any{"name": "Test"})
	if status != 200 {
		t.Fatalf("PUT location: %d", status)
	}

	// PUT a new EVSE under it.
	evseURL := locURL + "/EVSE99"
	status, _ = doJSON(t, "PUT", evseURL, map[string]any{"status": "AVAILABLE"})
	if status != 200 {
		t.Fatalf("PUT evse: %d", status)
	}
	loc := msp.State.Locations["NL/CPO/LOC9"]
	evses := asMapSlice(loc["evses"])
	if len(evses) != 1 || evses[0]["uid"] != "EVSE99" || evses[0]["status"] != "AVAILABLE" {
		t.Fatalf("EVSE not stored correctly: %v", evses)
	}

	// PATCH same EVSE — status flip, other fields preserved.
	status, _ = doJSON(t, "PATCH", evseURL, map[string]any{"status": "CHARGING"})
	if status != 200 {
		t.Fatalf("PATCH evse: %d", status)
	}
	evses = asMapSlice(msp.State.Locations["NL/CPO/LOC9"]["evses"])
	if evses[0]["status"] != "CHARGING" || evses[0]["uid"] != "EVSE99" {
		t.Fatalf("PATCH did not merge: %v", evses[0])
	}

	// PUT a connector under that EVSE.
	connURL := evseURL + "/1"
	status, _ = doJSON(t, "PUT", connURL, map[string]any{"standard": "IEC_62196_T2"})
	if status != 200 {
		t.Fatalf("PUT connector: %d", status)
	}
	evse := asMapSlice(msp.State.Locations["NL/CPO/LOC9"]["evses"])[0]
	conns := asMapSlice(evse["connectors"])
	if len(conns) != 1 || conns[0]["id"] != "1" || conns[0]["standard"] != "IEC_62196_T2" {
		t.Fatalf("connector not stored: %v", conns)
	}

	// Missing-parent cases return 404.
	status, _ = doJSON(t, "PATCH", msp.URL+"/ocpi/receiver/"+VersionV221+"/locations/NL/CPO/UNKNOWN/EVSE1", map[string]any{})
	if status != 404 {
		t.Errorf("expected 404 for missing location, got %d", status)
	}
	status, _ = doJSON(t, "PATCH", locURL+"/NOPE/1", map[string]any{})
	if status != 404 {
		t.Errorf("expected 404 for missing EVSE, got %d", status)
	}
}

func TestCPOSenderEVSEAndConnectorGET(t *testing.T) {
	cpo := startServer(t, RoleCPO)
	base := cpo.URL + "/ocpi/sender/" + VersionV221 + "/locations/NL/CPO/LOC001"

	body := getJSON(t, base+"/EVSE001")
	data, _ := body["data"].(map[string]any)
	if data["uid"] != "EVSE001" {
		t.Errorf("expected uid=EVSE001, got %v", data["uid"])
	}

	body = getJSON(t, base+"/EVSE001/1")
	data, _ = body["data"].(map[string]any)
	if data["id"] != "1" || data["standard"] != "IEC_62196_T2" {
		t.Errorf("connector GET wrong: %v", data)
	}
}

func TestChargingPreferencesV221Only(t *testing.T) {
	cpo := startServer(t, RoleCPO)
	msp := startServer(t, RoleMSP)

	// Seed a session on the CPO so charging_preferences has a target.
	cpo.State.mu.Lock()
	cpo.State.Sessions["NL/MSP/SESS1"] = map[string]any{
		"id":           "SESS1",
		"country_code": "NL",
		"party_id":     "MSP",
	}
	cpo.State.Counts.Sessions = 1
	cpo.State.mu.Unlock()

	prefsURL := cpo.URL + "/ocpi/sender/" + VersionV221 + "/sessions/NL/MSP/SESS1/charging_preferences"
	prefs := map[string]any{"profile_type": "GREEN", "departure_time": "2026-04-20T20:00:00Z"}
	status, body := doJSON(t, "PUT", prefsURL, prefs)
	if status != 200 {
		t.Fatalf("PUT charging_preferences: %d, body=%v", status, body)
	}
	data, _ := body["data"].(map[string]any)
	if data["result"] != "ACCEPTED" || data["profile_type"] != "GREEN" {
		t.Errorf("unexpected response: %v", data)
	}
	sess := cpo.State.Sessions["NL/MSP/SESS1"]
	stored, ok := sess["charging_preferences"].(map[string]any)
	if !ok || stored["profile_type"] != "GREEN" {
		t.Errorf("charging_preferences not stored on session: %v", sess)
	}

	// 2.1.1 must NOT expose the endpoint (it doesn't exist in that version).
	status, _ = doJSON(t, "PUT", cpo.URL+"/ocpi/sender/"+VersionV211+"/sessions/NL/MSP/SESS1/charging_preferences", prefs)
	if status != 404 {
		t.Errorf("expected 404 for charging_preferences on 2.1.1, got %d", status)
	}

	// Sanity: MSP side has no such endpoint either (CPO-only sub-object).
	status, _ = doJSON(t, "PUT", msp.URL+"/ocpi/sender/"+VersionV221+"/sessions/NL/MSP/SESS1/charging_preferences", prefs)
	if status != 404 {
		t.Errorf("expected 404 on MSP, got %d", status)
	}
}
