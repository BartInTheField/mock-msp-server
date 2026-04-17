package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	url := flag.String("url", "http://localhost:3010", "Public URL for OCPI endpoints")
	urlShort := flag.String("u", "", "alias for --url")
	port := flag.Int("port", 3010, "Server port")
	portShort := flag.Int("p", 0, "alias for --port")
	role := flag.String("role", RoleMSP, "OCPI role to mock: msp or cpo")
	roleShort := flag.String("r", "", "alias for --role")
	peer := flag.String("peer", "", "Peer base URL to auto-register with on startup (e.g. http://localhost:3011)")
	peerShort := flag.String("P", "", "alias for --peer")
	partyID := flag.String("party-id", "", "OCPI party_id (defaults: MSP for --role msp, CPO for --role cpo)")
	countryCode := flag.String("country-code", "NL", "OCPI country_code (ISO-3166 alpha-2)")
	flag.Parse()

	if *urlShort != "" {
		*url = *urlShort
	}
	if *portShort != 0 {
		*port = *portShort
	}
	if *roleShort != "" {
		*role = *roleShort
	}
	if *peerShort != "" {
		*peer = *peerShort
	}
	if *role != RoleMSP && *role != RoleCPO {
		fmt.Fprintf(os.Stderr, "invalid --role %q: must be %q or %q\n", *role, RoleMSP, RoleCPO)
		os.Exit(2)
	}
	if *partyID == "" {
		*partyID = DefaultPartyID(*role)
	}
	if len(*countryCode) != 2 {
		fmt.Fprintf(os.Stderr, "invalid --country-code %q: must be 2 characters\n", *countryCode)
		os.Exit(2)
	}

	// Channels bridge server goroutine -> TUI.
	logCh := make(chan LogEntry, 256)
	stateCh := make(chan struct{}, 256)

	onLog := func(e LogEntry) {
		select {
		case logCh <- e:
		default:
		}
	}
	onStateChange := func() {
		select {
		case stateCh <- struct{}{}:
		default:
		}
	}

	srv := NewServer(*role, *partyID, *countryCode, *url, *port, onLog, onStateChange)

	go func() {
		if err := srv.ListenAndServe(); err != nil {
			fmt.Fprintln(os.Stderr, "server error:", err)
			os.Exit(1)
		}
	}()

	m := newModel(srv, *url, *port, *peer, logCh, stateCh)
	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		log.Fatal(err)
	}
}
