# FakeGo

A lightweight fake SMTP server written in Go, designed for local development and testing. Captures outgoing emails instead of delivering them and exposes them via a web UI and REST API.

## Features

- SMTP server that accepts all incoming mail without authentication
- Web UI at `http://localhost:1080` for browsing captured emails
- REST API and Server-Sent Events for real-time email notifications
- Optional in-memory mode (no disk writes)
- Configurable relay domain filtering

## Usage

```
go run . [flags]
```

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--port` | `-p` | `25` | SMTP port |
| `--bind-address` | `-a` | *(all interfaces)* | IP/hostname to bind SMTP to |
| `--output-dir` | `-o` | `received-emails` | Directory to save captured emails |
| `--relay-domains` | `-r` | *(all)* | Comma-separated domains to accept (accepts all if unset) |
| `--memory-mode` | `-m` | `false` | Keep emails in memory only, do not write to disk |
| `--web-port` | `-w` | `1080` | Port for the web UI |

### Examples

```bash
# Run with defaults (SMTP on :25, web UI on :1080)
go run .

# Use an unprivileged port and memory mode
go run . -p 2525 -m

# Accept mail only for specific domains
go run . -p 2525 -r example.com,test.local
```

## API

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/emails` | List all captured emails |
| `DELETE` | `/api/emails` | Delete all captured emails |
| `GET` | `/api/emails/{id}` | Get a single email with parsed HTML body |
| `GET` | `/api/events` | SSE stream of incoming email events |
| `GET` | `/api/status` | Server status (SMTP address) |

## Building

```bash
go build -o fakego .
```

## Requirements

- Go 1.21+
