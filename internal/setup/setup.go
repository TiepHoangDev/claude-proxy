// Package setup serves the configuration page that helps a user point their
// Claude client at this proxy and configure optional alternate-provider
// routing (internal/router) without editing config.json by hand.
package setup

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"os"
	"runtime"

	"claude-proxy/internal/router"
)

//go:embed static/setup.html
var setupHTML []byte

const configPath = "config.json"

// Handler serves the static setup page. Data is fetched client-side from
// StatusAPIHandler via Alpine.js.
func Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(setupHTML)
	}
}

type statusResponse struct {
	OS         string         `json:"os"`
	Port       string         `json:"port"`
	ProxyURL   string         `json:"proxyUrl"`
	Configured bool           `json:"configured"`
	Config     *router.Config `json:"config"`
}

// StatusAPIHandler returns the host OS, proxy URL, whether config.json
// exists yet, and the current routing config.
func StatusAPIHandler(holder *router.Holder, port string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, err := os.Stat(configPath)
		configured := err == nil

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(statusResponse{
			OS:         runtime.GOOS,
			Port:       port,
			ProxyURL:   "http://localhost:" + port,
			Configured: configured,
			Config:     holder.Get(),
		})
	}
}

type resultResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

func writeResult(w http.ResponseWriter, ok bool, message string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resultResponse{OK: ok, Message: message})
}

// SaveAPIHandler decodes a router.Config from the request body, persists it
// to config.json, and applies it immediately via holder.
func SaveAPIHandler(holder *router.Holder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var cfg router.Config
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeResult(w, false, "invalid request body: "+err.Error())
			return
		}
		if err := holder.SetAndSave(configPath, &cfg); err != nil {
			writeResult(w, false, "failed to save config: "+err.Error())
			return
		}
		writeResult(w, true, "saved")
	}
}

// TestDeepSeekAPIHandler decodes a router.DeepSeekConfig from the request
// body and verifies that the configured endpoint and API key work.
func TestDeepSeekAPIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var cfg router.DeepSeekConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeResult(w, false, "invalid request body: "+err.Error())
			return
		}
		ok, msg := router.TestDeepSeek(&cfg)
		writeResult(w, ok, msg)
	}
}

// TestOpenRouterAPIHandler decodes a router.OpenRouterConfig from the
// request body and verifies that the configured endpoint and API key work.
func TestOpenRouterAPIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var cfg router.OpenRouterConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeResult(w, false, "invalid request body: "+err.Error())
			return
		}
		ok, msg := router.TestOpenRouter(&cfg)
		writeResult(w, ok, msg)
	}
}

type modelsResponse struct {
	OK      bool     `json:"ok"`
	Models  []string `json:"models,omitempty"`
	Message string   `json:"message,omitempty"`
}

// ModelsAPIHandler returns the list of model slugs available for provider
// (currently only "openrouter" is supported; other providers don't expose a
// public model-listing endpoint).
func ModelsAPIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("provider") {
		case "openrouter":
			models, err := router.FetchOpenRouterModels(r.URL.Query().Get("base_url"))
			if err != nil {
				_ = json.NewEncoder(w).Encode(modelsResponse{OK: false, Message: err.Error()})
				return
			}
			_ = json.NewEncoder(w).Encode(modelsResponse{OK: true, Models: models})
		default:
			_ = json.NewEncoder(w).Encode(modelsResponse{OK: false, Message: "unsupported provider"})
		}
	}
}
