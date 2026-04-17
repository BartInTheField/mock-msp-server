package main

import (
	"net"
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
	msp.State.mu.RUnlock()
	if creds == nil {
		t.Fatal("MSP has no PeerCredentials after Register")
	}
	if bd, _ := creds["business_details"].(map[string]any); bd == nil || bd["name"] != "Mock CPO" {
		t.Errorf("expected CPO business_details.name=Mock CPO, got %v", creds["business_details"])
	}

	// CPO should now have MSP credentials stored (the PUT side of the handshake)
	cpo.State.mu.RLock()
	cpoPeer := cpo.State.PeerCredentials
	cpo.State.mu.RUnlock()
	if cpoPeer == nil {
		t.Fatal("CPO has no PeerCredentials after receiving PUT")
	}
	if bd, _ := cpoPeer["business_details"].(map[string]any); bd == nil || bd["name"] != "Mock MSP" {
		t.Errorf("expected MSP business_details.name=Mock MSP, got %v", cpoPeer["business_details"])
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
	if bd, _ := creds["business_details"].(map[string]any); bd == nil || bd["name"] != "Mock MSP" {
		t.Errorf("expected MSP business_details.name=Mock MSP, got %v", creds["business_details"])
	}
}
