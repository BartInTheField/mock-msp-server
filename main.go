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
	flag.Parse()

	if *urlShort != "" {
		*url = *urlShort
	}
	if *portShort != 0 {
		*port = *portShort
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

	srv := NewServer(*url, *port, onLog, onStateChange)

	go func() {
		if err := srv.ListenAndServe(); err != nil {
			fmt.Fprintln(os.Stderr, "server error:", err)
			os.Exit(1)
		}
	}()

	m := newModel(srv, *url, *port, logCh, stateCh)
	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		log.Fatal(err)
	}
}
