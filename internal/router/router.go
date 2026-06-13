// Package router decides whether a request should be forwarded to an
// alternate API provider (e.g. DeepSeek) instead of api.anthropic.com,
// based on the request path and the model field in the request body.
package router

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
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
