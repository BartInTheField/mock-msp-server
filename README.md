# Mock MSP — OCPI 2.1.1 Mock Mobility Service Provider

A mock Mobility Service Provider (MSP) server implementing the [OCPI 2.1.1](https://github.com/ocpi/ocpi/tree/release-2.1.1-bugfixes) protocol. Useful for testing and developing Charge Point Operator (CPO) integrations without needing a real MSP backend.

The server provides an interactive terminal UI (TUI) for real-time monitoring of requests, browsing stored data, and pulling modules from connected CPOs.

## Features

- Full OCPI 2.1.1 credential handshake (registration flow)
- Sender interface for **Tokens**
- Receiver interfaces for **Locations**, **Sessions**, **CDRs**, **Tariffs**, and **Commands**
- Pull data from connected CPO endpoints via OCPI discovery
- Interactive TUI dashboard with live request logging and data browsing
- In-memory data storage with pre-configured RFID tokens

## Prerequisites

- [Bun](https://bun.sh) runtime

## Getting Started

```bash
# Install dependencies
bun install

# Start the server (development mode with file watching)
bun run dev

# Or start without watch mode
bun run start
```

### CLI Options

| Flag | Description | Default |
|------|-------------|---------|
| `--port`, `-p` | Server port | `3010` |
| `--url`, `-u` | Public URL for OCPI endpoints | `http://localhost:3010` |

Example:

```bash
bun run dev --port 8080 --url https://my-msp.example.com
```

## Using with ngrok

To expose your local Mock MSP to external CPO servers (e.g. for testing against a staging environment), use [ngrok](https://ngrok.com):

```bash
# 1. Start an ngrok tunnel on the same port as the server
ngrok http 3010

# 2. Copy the forwarding URL from ngrok (e.g. https://ab12-34-56-78.ngrok-free.app)

# 3. Start the server with the ngrok URL so OCPI endpoints advertise the correct public address
bun run dev --url https://ab12-34-56-78.ngrok-free.app
```

The `--url` flag ensures that the versions and credentials endpoints return the ngrok URL instead of `localhost`, so the CPO can reach your server over the internet.

## How It Works

1. Start the server — it listens for incoming OCPI requests and displays the TUI dashboard.
2. A CPO registers by sending `PUT /ocpi/cpo/2.1.1/credentials` with its credentials.
3. The server responds with its own credentials, completing the OCPI handshake.
4. Once connected, use the TUI to pull locations, sessions, tariffs, and CDRs from the CPO.
5. The CPO can also push data to the server's receiver endpoints.

### Default MSP Identity

| Field | Value |
|-------|-------|
| Party ID | `MFC` |
| Country Code | `NL` |
| Token | `mocked-msp-token` |

Two pre-configured RFID tokens (`valid-token-1`, `valid-token-2`) are available for authorization testing.

## TUI Navigation

| Key | Action |
|-----|--------|
| `j` / `k` | Navigate up/down |
| `Enter` | Select item |
| `q` | Quit |

## Project Structure

```
src/
  index.tsx    # Entry point — CLI arg parsing, TUI setup
  server.ts    # Express server with OCPI endpoint handlers
  App.tsx      # React-based TUI dashboard component
```

## License

MIT
