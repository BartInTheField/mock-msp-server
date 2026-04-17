# Mock OCPI — 2.1.1 Mock MSP & CPO

A mock server that can play either side of the [OCPI 2.1.1](https://github.com/ocpi/ocpi/tree/release-2.1.1-bugfixes) protocol: a **Mobility Service Provider (MSP)** or a **Charge Point Operator (CPO)**. Useful for testing and developing integrations without needing a real counterparty.

The server provides an interactive terminal UI (TUI) for real-time monitoring of requests, browsing stored data, and pulling modules from the connected peer.

## Features

- Full OCPI 2.1.1 credential handshake (registration flow)
- **MSP mode** — sender interface for Tokens; receiver interfaces for Locations, Sessions, CDRs, Tariffs, Commands
- **CPO mode** — sender interfaces for Locations, Sessions, CDRs, Tariffs; receiver interfaces for Tokens and Commands
- Pull data from the peer via OCPI version discovery
- Interactive TUI dashboard with live request logging and data browsing
- In-memory data storage with pre-configured mock data per role

## Prerequisites

- [Go](https://go.dev) 1.22+

## Getting Started

```bash
# Fetch dependencies
go mod tidy

# Run in MSP mode (default)
go run .

# Or build a binary
go build -o mock-msp . && ./mock-msp
```

### CLI Options

| Flag | Description | Default |
|------|-------------|---------|
| `--role`, `-r` | OCPI role to mock: `msp` or `cpo` | `msp` |
| `--port`, `-p` | Server port | `3010` |
| `--url`, `-u` | Public URL for OCPI endpoints | `http://localhost:3010` |
| `--peer`, `-P` | Peer base URL to auto-register with on startup (e.g. `http://localhost:3011`). In the TUI, press `R` to re-register. | _unset_ |
| `--party-id` | OCPI `party_id` | `MSP` for `--role msp`, `CPO` for `--role cpo` |
| `--country-code` | OCPI `country_code` (ISO-3166 alpha-2) | `NL` |

Examples:

```bash
# Mock an MSP on port 8080
go run . --port 8080 --url https://my-msp.example.com

# Mock a CPO on port 3011
go run . --role cpo --port 3011 --url http://localhost:3011

# Mock an MSP that auto-registers with a CPO running on :3011
go run . --role msp --peer http://localhost:3011
```

### End-to-end test with two instances

Run a CPO in one terminal and an MSP in another — the MSP does the credentials
handshake automatically via `--peer`:

```bash
# Terminal 1
./mock-msp --role cpo --port 3011 --url http://localhost:3011

# Terminal 2
./mock-msp --role msp --port 3010 --url http://localhost:3010 --peer http://localhost:3011
```

After a moment both TUIs will show each other under `Registered`. Press `[1]`
in the MSP TUI to pull Locations from the CPO, `[1]` in the CPO TUI to pull
Tokens from the MSP, or `[R]` on either side to redo the handshake.

## Using with ngrok

To expose the server to external systems, use [ngrok](https://ngrok.com):

```bash
# 1. Start an ngrok tunnel on the same port as the server
ngrok http 3010

# 2. Start the server with the ngrok URL so OCPI endpoints advertise the correct public address
go run . --url https://ab12-34-56-78.ngrok-free.app
```

The `--url` flag ensures that the versions and credentials endpoints return the ngrok URL instead of `localhost`, so the peer can reach the server over the internet.

## How It Works

1. Start the server — it listens for incoming OCPI requests and displays the TUI dashboard.
2. The peer registers by sending `PUT /ocpi/2.1.1/credentials` with its credentials.
3. The server responds with its own credentials, completing the OCPI handshake.
4. Once connected, use the TUI to pull modules from the peer:
   - **MSP mode:** pull locations, sessions, CDRs, tariffs, and tokens from the CPO.
   - **CPO mode:** pull tokens from the MSP.
5. The peer can also push data to the server's receiver endpoints.

### MSP Mode

Default identity (override with `--party-id` / `--country-code`):

| Field | Value |
|-------|-------|
| Party ID | `MSP` |
| Country Code | `NL` |
| Token | `mocked-msp-token` |

Two pre-configured RFID tokens (`valid-token-1`, `valid-token-2`) are available for authorization testing.

### CPO Mode

Default identity (override with `--party-id` / `--country-code`):

| Field | Value |
|-------|-------|
| Party ID | `CPO` |
| Country Code | `NL` |
| Token | `mocked-cpo-token` |

Pre-populated data (visible via MSP-side pulls):

- One mock location (`NL/CPO/LOC001`) with an AVAILABLE EVSE and connector.
- One mock tariff (`NL/CPO/TARIFF001`, €0.25/kWh).

MSPs can push tokens via `PUT /ocpi/receiver/2.1.1/tokens/{countryCode}/{partyId}/{tokenUid}` and send commands via `POST /ocpi/receiver/2.1.1/commands/{command}`.

## TUI Navigation

| Key | Action |
|-----|--------|
| `j` / `k` | Navigate up/down |
| `Enter` | Select item |
| `p` | Pull current module |
| `a` | Pull all modules |
| `c` | Clear logs |
| `Esc` | Go back |
| `q` | Quit |

## Project Structure

```
main.go      # Entry point — CLI flag parsing, server + TUI wiring
server.go    # net/http server with role-based OCPI endpoint handlers
pull.go      # Peer endpoint discovery and pull-module logic
tui.go       # Bubble Tea TUI dashboard
```

## License

MIT
