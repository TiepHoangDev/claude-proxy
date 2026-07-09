# Claude Proxy Monitor

One-click start, stop, and configure the [claude-proxy](https://github.com/TiepHoangDev/claude-proxy) reverse proxy without leaving VS Code.

The extension auto-downloads the latest release binary, starts the proxy locally, and sets `ANTHROPIC_BASE_URL` in your integrated terminal so Claude tools (Claude Code, Claude Agent SDK) route through the proxy transparently.

## Features

- **Auto-download** — fetches the correct platform binary from GitHub Releases on first start
- **Status bar** — running state, port, rate-limit usage, and DeepSeek balance at a glance
- **One-click commands** — Start, Stop, Restart, Open Dashboard, Open Setup, Show Logs, Check for Update
- **Health monitoring** — polls the proxy's health endpoint every 60s and surfaces rate-limit warnings and Claude subscription usage in the status bar
- **Auto-config** — sets `ANTHROPIC_BASE_URL` in your terminal environment on start, restores it on stop

## Commands

Open the Command Palette (`Ctrl+Shift+P`) and type "Claude Proxy":

| Command | Description |
|---|---|
| **Start** | Download the latest binary and start the proxy |
| **Stop** | Stop the running proxy |
| **Restart** | Stop then start again |
| **Open Dashboard** | Open the request dashboard in your browser |
| **Open Setup / Config** | Open the config page (routing rules, API keys) |
| **Show Logs** | View proxy output in the VS Code Output panel |
| **Check for Binary Update** | Stop the proxy, download the latest binary, restart |

## Settings

- `claudeProxy.port` — port the proxy listens on (default `8080`)
- `claudeProxy.autoStart` — start the proxy automatically when VS Code opens (default `false`)
- `claudeProxy.devBinary` — path to a local dev binary; when set, skips GitHub download entirely

## Requirements

- Windows or Linux (amd64)
- No other process on the configured port

## GitHub

https://github.com/TiepHoangDev/claude-proxy
