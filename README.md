# claude-proxy

A Go reverse proxy for the Claude API (`api.anthropic.com`) that tracks token
usage, tool calls, and conversation content for every request, with a live
dashboard for inspection.

## Features

- Transparent reverse proxy to `https://api.anthropic.com`, including SSE streaming
- Per-request tracking of input/output/cache tokens, model, status, duration, and tool calls
- Live dashboard with request list and per-request conversation timeline
- Optional routing rules (`config.json`): redirect matching models to an
  alternate provider (e.g. DeepSeek) and/or inject extra system-prompt text

## Demo

### Request list

![Dashboard](docs/dashboard.png)

### Request detail / conversation timeline

![Request detail](docs/detail.png)

## Usage

```bash
go build -o proxy.exe ./cmd/proxy   # build
go run ./cmd/proxy                  # run (default port 8080)
```

Or via npm scripts: `npm run build`, `npm start`.

Point your Claude client at `http://localhost:8080` instead of
`https://api.anthropic.com`, then open the dashboard at
`http://localhost:8080/_proxy/dashboard`.

### Environment variables

- `PORT` — listen port (default `8080`)
- `NO_BROWSER=1` — don't auto-open the dashboard on startup

### Optional routing config

Copy `config.example.json` to `config.json` to enable alternate-provider
routing and/or system-prompt injection for matching models.

## Development

```bash
go vet ./...   # static checks
go test ./...  # run tests
```
