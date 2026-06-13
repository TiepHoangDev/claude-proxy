package router

import (
	"encoding/json"
	"testing"
)

func testConfig() *Config {
	return &Config{
		DeepSeek: &DeepSeekConfig{
			APIKey:  "sk-test",
			BaseURL: "https://api.deepseek.com/anthropic",
			Model:   "deepseek-v4-pro",
			Match:   "haiku",
		},
	}
}

func TestResolveMatches(t *testing.T) {
	c := testConfig()
	target := c.Resolve("/v1/messages", "claude-haiku-4-5-20251001")
	if target == nil {
		t.Fatal("expected non-nil target for haiku model")
	}
	if target.Scheme != "https" || target.Host != "api.deepseek.com" {
		t.Errorf("unexpected scheme/host: %s %s", target.Scheme, target.Host)
	}
	if target.PathPrefix != "/anthropic" {
		t.Errorf("unexpected path prefix: %q", target.PathPrefix)
	}
	if target.APIKeyHeader != "x-api-key" || target.APIKey != "sk-test" {
		t.Errorf("unexpected api key header/value: %s %s", target.APIKeyHeader, target.APIKey)
	}
	if target.Model != "deepseek-v4-pro" {
		t.Errorf("unexpected model: %s", target.Model)
	}
}

func TestResolveCaseInsensitive(t *testing.T) {
	c := testConfig()
	if c.Resolve("/v1/messages", "claude-HAIKU-4-5") == nil {
		t.Error("expected case-insensitive match")
	}
}

func TestResolveNoMatchModel(t *testing.T) {
	c := testConfig()
	if target := c.Resolve("/v1/messages", "claude-sonnet-4-5-20251001"); target != nil {
		t.Errorf("expected nil target for non-haiku model, got %+v", target)
	}
}

func TestResolveNoMatchPath(t *testing.T) {
	c := testConfig()
	if target := c.Resolve("/v1/complete", "claude-haiku-4-5"); target != nil {
		t.Errorf("expected nil target for non-messages path, got %+v", target)
	}
}

func TestResolveNilConfig(t *testing.T) {
	var c *Config
	if target := c.Resolve("/v1/messages", "claude-haiku-4-5"); target != nil {
		t.Errorf("expected nil target for nil config, got %+v", target)
	}
}

func TestResolveEmptyConfig(t *testing.T) {
	c := &Config{}
	if target := c.Resolve("/v1/messages", "claude-haiku-4-5"); target != nil {
		t.Errorf("expected nil target when DeepSeek config absent, got %+v", target)
	}
}

func TestRewriteModel(t *testing.T) {
	body := []byte(`{"model":"claude-haiku-4-5-20251001","max_tokens":1024,"messages":[{"role":"user","content":"hi"}]}`)
	out, err := RewriteModel(body, "deepseek-v4-pro")
	if err != nil {
		t.Fatalf("RewriteModel error: %v", err)
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	var model string
	if err := json.Unmarshal(got["model"], &model); err != nil {
		t.Fatalf("model field not a string: %v", err)
	}
	if model != "deepseek-v4-pro" {
		t.Errorf("model = %q, want deepseek-v4-pro", model)
	}

	if _, ok := got["max_tokens"]; !ok {
		t.Error("max_tokens field lost during rewrite")
	}
	if _, ok := got["messages"]; !ok {
		t.Error("messages field lost during rewrite")
	}
}

func TestRewriteModelInvalidJSON(t *testing.T) {
	if _, err := RewriteModel([]byte("not json"), "deepseek-v4-pro"); err == nil {
		t.Error("expected error for invalid JSON input")
	}
}

func TestInjectSystemMatch(t *testing.T) {
	c := &Config{SystemInject: &SystemInjectConfig{Match: "haiku", Text: "ask if unsure"}}
	text, ok := c.InjectSystem("claude-HAIKU-4-5")
	if !ok || text != "ask if unsure" {
		t.Errorf("got (%q, %v), want (\"ask if unsure\", true)", text, ok)
	}
}

func TestInjectSystemNoMatch(t *testing.T) {
	c := &Config{SystemInject: &SystemInjectConfig{Match: "haiku", Text: "ask if unsure"}}
	if _, ok := c.InjectSystem("claude-sonnet-4-5"); ok {
		t.Error("expected no injection for non-matching model")
	}
}

func TestInjectSystemUnconfigured(t *testing.T) {
	var c *Config
	if _, ok := c.InjectSystem("claude-haiku-4-5"); ok {
		t.Error("expected no injection for nil config")
	}
	c = &Config{}
	if _, ok := c.InjectSystem("claude-haiku-4-5"); ok {
		t.Error("expected no injection for empty config")
	}
}

func TestInjectSystemPromptAbsent(t *testing.T) {
	body := []byte(`{"model":"claude-haiku-4-5","messages":[{"role":"user","content":"hi"}]}`)
	out, err := InjectSystemPrompt(body, "extra instructions")
	if err != nil {
		t.Fatalf("InjectSystemPrompt error: %v", err)
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	var sys string
	if err := json.Unmarshal(got["system"], &sys); err != nil {
		t.Fatalf("system field not a string: %v", err)
	}
	if sys != "extra instructions" {
		t.Errorf("system = %q, want %q", sys, "extra instructions")
	}
}

func TestInjectSystemPromptStringExisting(t *testing.T) {
	body := []byte(`{"model":"claude-haiku-4-5","system":"be concise","messages":[]}`)
	out, err := InjectSystemPrompt(body, "extra instructions")
	if err != nil {
		t.Fatalf("InjectSystemPrompt error: %v", err)
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	var sys string
	if err := json.Unmarshal(got["system"], &sys); err != nil {
		t.Fatalf("system field not a string: %v", err)
	}
	want := "be concise\n\nextra instructions"
	if sys != want {
		t.Errorf("system = %q, want %q", sys, want)
	}
}

func TestInjectSystemPromptArrayExisting(t *testing.T) {
	body := []byte(`{"model":"claude-haiku-4-5","system":[{"type":"text","text":"be concise"}],"messages":[]}`)
	out, err := InjectSystemPrompt(body, "extra instructions")
	if err != nil {
		t.Fatalf("InjectSystemPrompt error: %v", err)
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	var blocks []map[string]string
	if err := json.Unmarshal(got["system"], &blocks); err != nil {
		t.Fatalf("system field not an array: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("len(blocks) = %d, want 2", len(blocks))
	}
	if blocks[0]["text"] != "be concise" {
		t.Errorf("blocks[0] = %v, want existing block preserved", blocks[0])
	}
	if blocks[1]["type"] != "text" || blocks[1]["text"] != "extra instructions" {
		t.Errorf("blocks[1] = %v, want appended text block", blocks[1])
	}
}

func TestInjectSystemPromptInvalidJSON(t *testing.T) {
	if _, err := InjectSystemPrompt([]byte("not json"), "extra"); err == nil {
		t.Error("expected error for invalid JSON input")
	}
}
