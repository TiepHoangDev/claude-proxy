# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Style rules

See [RULES.md](RULES.md) for project-specific coding style rules. Follow these strictly.

## What this is

A Go reverse proxy that forwards all requests to `https://api.anthropic.com`, tracking token usage, tool calls, and conversation content, and exposing a live dashboard with per-request detail views.

## Commands

```bash
go build -o proxy.exe ./cmd/proxy   # build
go vet ./...                         # static checks
go test ./...                        # run tests
go run ./cmd/proxy                   # run (default port 8080)
```

Run a single test: `go test ./internal/stats/ -run TestBlocksFromContent`

Or via npm scripts (package.json wraps the same commands): `npm run build`, `npm start`, `npm run vet`, `npm run clean`.

- `PORT` env var overrides the listen port (default `8080`).
- `NO_BROWSER=1` disables auto-opening the dashboard in the browser on startup.

## Architecture

- **cmd/proxy/main.go** — sets up `httputil.ReverseProxy` targeting `api.anthropic.com` (`FlushInterval = -1` for SSE streaming). Routes via `http.ServeMux`:
  - `/_proxy/dashboard`, `/_proxy/api/requests` — request list page and JSON (`dashboard.Handler`, `dashboard.APIHandler`)
  - `/_proxy/requests/{id}`, `/_proxy/api/requests/{id}` — per-request detail page and JSON (`dashboard.DetailHandler`, `dashboard.DetailAPIHandler`)
  - `/` — everything else goes through `statsMiddleware` → proxy

  `statsMiddleware` reads and re-sets the request body (`io.NopCloser`) to extract the request model and any `tool_result` blocks before forwarding, wraps the response in a `CaptureWriter`, and on completion builds a `stats.RequestLog` that is stored in the in-memory `Store` and appended to `request.log` via a `FileLogger`. Tool uses/results are also appended to `tools.log`. Stored request/response bodies are pretty-printed and capped (`capBody`/`maxStoredBody`); `CaptureWriter` itself buffers up to `captureBodyCap` (128KB) of the response for parsing/display.

  On startup, opens the dashboard URL in the default browser after a short delay (unless `NO_BROWSER` is set).

- **internal/stats/capture.go** — `CaptureWriter` wraps the `http.ResponseWriter` to extract token usage, tool calls, and content blocks without breaking streaming:
  - For `text/event-stream` (streaming) responses, parses SSE `data:` lines incrementally line-by-line as bytes are written: `message_start` (input/cache tokens + model), `message_delta` (output/cache tokens), and `content_block_start`/`content_block_delta`/`content_block_stop` (accumulates `tool_use`, `text`, and `thinking` blocks into `pendingBlock`s, emitted as `ToolUse`s and `TimelineBlock`s).
  - For non-streaming responses, buffers the body and parses it as JSON for usage/model/tool_use/content blocks after the response completes (`Finalize()`).
  - Also implements `http.Flusher` so streaming passthrough keeps working.
  - `ExtractRequestModel` pulls `model` from the request body as a fallback when the response doesn't include it.

- **internal/stats/tools.go** — conversation/content-block model shared by capture and dashboard:
  - `ToolUse`, `ToolResult`, `TimelineBlock`, `TimelineEntry` types.
  - `BuildRequestTimeline` parses a Messages API request body (`system` + `messages`) into ordered `TimelineEntry`s; `BlocksFromResponse` does the same for a non-streaming response's `content`.
  - `ExtractToolUses`/`ExtractToolResults` scan response/request bodies for `tool_use`/`tool_result` blocks; `LastUserText` finds the most recent user text turn (skipping pure-`tool_result` turns) for dashboard previews.

- **internal/stats/store.go** — `Store` is an in-memory, mutex-protected ring buffer (capacity 200) of `RequestLog` entries, each with an auto-incrementing `ID`. `Get(id)` looks up a single entry for the detail page; `Totals()` aggregates token counts across stored entries. Resets on restart — persistence lives only in `request.log`/`tools.log`.

- **internal/stats/filelog.go** — `FileLogger` appends JSON-encoded entries as lines to a file, trimming to the most recent N lines on each write. Used for `request.log` (100 entries) and `tools.log` (500 entries).

- **internal/dashboard/** — `Handler`/`APIHandler` serve the request-list page (`static/index.html`) and its JSON data (with computed `UserPreview`/`ToolNames` fields); `DetailHandler`/`DetailAPIHandler` serve the per-request detail page (`static/detail.html`) and its JSON, including the full conversation `Timeline`. Pages are static HTML (Alpine.js) that fetch data client-side; the list page auto-refreshes every 5s.

## Key constraints

- Request bodies are fully read and re-set (`io.NopCloser`) so the body can be inspected for `model` and `tool_result` blocks before being forwarded.
- Streaming responses must never be fully buffered; any new extraction logic on the response side should go through the incremental SSE line parser in `CaptureWriter.processSSE`, not a full-body buffer.
- Anthropic streaming usage shape: `message_start.message.usage.input_tokens`/cache fields for input tokens, `message_delta.usage.output_tokens` for (cumulative) output tokens.
- Tests exist under `internal/stats` and `internal/dashboard` (`*_test.go`); run `go test ./...` before considering a change complete.
