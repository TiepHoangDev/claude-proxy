// Package router decides whether a request should be forwarded to an
// alternate API provider (e.g. DeepSeek) instead of api.anthropic.com,
// based on the request path and the model field in the request body.
package router

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DeepSeekConfig holds the settings for routing requests to DeepSeek's
// Anthropic-compatible API.
type DeepSeekConfig struct {
	APIKey  string `json:"api_key"`
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
	Match   string `json:"match"`
}

// SystemInjectConfig holds settings for appending extra text to the
// "system" prompt of requests whose model matches Match.
type SystemInjectConfig struct {
	Match string `json:"match"`
	Text  string `json:"text"`
}

// Config is the top-level routing configuration.
type Config struct {
	DeepSeek     *DeepSeekConfig     `json:"deepseek"`
	SystemInject *SystemInjectConfig `json:"system_inject"`
}

// Target describes where and how a request should be forwarded.
type Target struct {
	Scheme       string
	Host         string
	PathPrefix   string
	APIKeyHeader string
	APIKey       string
	Model        string
}

// Load reads routing configuration from path. A missing file is not an
// error: it returns an empty *Config, which causes Resolve to always
// return nil (no alternate routing).
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Save normalizes cfg (clearing the DeepSeek/SystemInject sections if all of
// their fields are empty, so Resolve/InjectSystem treat them as disabled)
// and writes it as indented JSON to path.
func Save(path string, cfg *Config) error {
	normalized := *cfg
	if normalized.DeepSeek != nil && *normalized.DeepSeek == (DeepSeekConfig{}) {
		normalized.DeepSeek = nil
	}
	if normalized.SystemInject != nil && *normalized.SystemInject == (SystemInjectConfig{}) {
		normalized.SystemInject = nil
	}

	data, err := json.MarshalIndent(&normalized, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// Holder provides thread-safe access to a routing Config that can be
// reloaded and saved at runtime (e.g. from the setup page) without
// restarting the server.
type Holder struct {
	mu  sync.RWMutex
	cfg *Config
}

// NewHolder loads the config at path (see Load) and wraps it in a Holder.
func NewHolder(path string) (*Holder, error) {
	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}
	return &Holder{cfg: cfg}, nil
}

// Get returns the current config.
func (h *Holder) Get() *Config {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.cfg
}

// SetAndSave persists cfg to path and, on success, makes it the current
// config returned by Get.
func (h *Holder) SetAndSave(path string, cfg *Config) error {
	if err := Save(path, cfg); err != nil {
		return err
	}
	h.mu.Lock()
	h.cfg = cfg
	h.mu.Unlock()
	return nil
}

// TestDeepSeek sends a minimal Messages API request to cfg's base URL to
// verify that the API key and endpoint work. It always returns a
// human-readable message; ok is true only for a successful (200) response.
func TestDeepSeek(cfg *DeepSeekConfig) (ok bool, message string) {
	if cfg == nil || cfg.APIKey == "" || cfg.BaseURL == "" {
		return false, "API key and base URL are required"
	}

	model := cfg.Model
	if model == "" {
		model = "deepseek-chat"
	}

	body, err := json.Marshal(map[string]any{
		"model":      model,
		"max_tokens": 1,
		"messages":   []map[string]string{{"role": "user", "content": "ping"}},
	})
	if err != nil {
		return false, fmt.Sprintf("failed to build test request: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(cfg.BaseURL, "/")+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return false, fmt.Sprintf("invalid base URL: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", cfg.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Sprintf("request failed: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))

	switch resp.StatusCode {
	case http.StatusOK:
		return true, "OK: API key is valid"
	case http.StatusUnauthorized, http.StatusForbidden:
		return false, "invalid API key (HTTP " + strconv.Itoa(resp.StatusCode) + ")"
	default:
		return false, fmt.Sprintf("unexpected response: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
}

// Resolve returns a non-nil *Target if path/model should be routed to
// DeepSeek, or nil if the request should go to api.anthropic.com as usual.
func (c *Config) Resolve(path, model string) *Target {
	if c == nil || c.DeepSeek == nil {
		return nil
	}
	if path != "/v1/messages" {
		return nil
	}
	if c.DeepSeek.Match == "" || !strings.Contains(strings.ToLower(model), strings.ToLower(c.DeepSeek.Match)) {
		return nil
	}

	u, err := url.Parse(c.DeepSeek.BaseURL)
	if err != nil {
		return nil
	}

	return &Target{
		Scheme:       u.Scheme,
		Host:         u.Host,
		PathPrefix:   u.Path,
		APIKeyHeader: "x-api-key",
		APIKey:       c.DeepSeek.APIKey,
		Model:        c.DeepSeek.Model,
	}
}

// RewriteModel returns body with its top-level "model" field replaced by
// newModel.
func RewriteModel(body []byte, newModel string) ([]byte, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(newModel)
	if err != nil {
		return nil, err
	}
	m["model"] = encoded
	return json.Marshal(m)
}

// InjectSystem returns the text to append to the system prompt for model,
// and whether any injection should happen at all.
func (c *Config) InjectSystem(model string) (string, bool) {
	if c == nil || c.SystemInject == nil || c.SystemInject.Text == "" {
		return "", false
	}
	if c.SystemInject.Match == "" || !strings.Contains(strings.ToLower(model), strings.ToLower(c.SystemInject.Match)) {
		return "", false
	}
	return c.SystemInject.Text, true
}

// InjectSystemPrompt returns body with text appended to its top-level
// "system" field. The "system" field may be absent, a plain string, or an
// array of content blocks (per the Messages API); in each case text is
// added without discarding the existing system prompt.
func InjectSystemPrompt(body []byte, text string) ([]byte, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}

	existing, ok := m["system"]
	if !ok {
		encoded, err := json.Marshal(text)
		if err != nil {
			return nil, err
		}
		m["system"] = encoded
		return json.Marshal(m)
	}

	var s string
	if err := json.Unmarshal(existing, &s); err == nil {
		encoded, err := json.Marshal(s + "\n\n" + text)
		if err != nil {
			return nil, err
		}
		m["system"] = encoded
		return json.Marshal(m)
	}

	var blocks []json.RawMessage
	if err := json.Unmarshal(existing, &blocks); err == nil {
		block, err := json.Marshal(map[string]string{"type": "text", "text": text})
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
		encoded, err := json.Marshal(blocks)
		if err != nil {
			return nil, err
		}
		m["system"] = encoded
		return json.Marshal(m)
	}

	return nil, fmt.Errorf("unsupported system field type")
}
