package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

func TestResolveMatchesOpenRouter(t *testing.T) {
	c := &Config{
		OpenRouter: &OpenRouterConfig{
			APIKey:  "sk-or-test",
			BaseURL: "https://openrouter.ai/api",
			Model:   "anthropic/claude-haiku-4.5",
			Match:   "haiku",
		},
	}
	target := c.Resolve("/v1/messages", "claude-haiku-4-5-20251001")
	if target == nil {
		t.Fatal("expected non-nil target for haiku model")
	}
	if target.Scheme != "https" || target.Host != "openrouter.ai" {
		t.Errorf("unexpected scheme/host: %s %s", target.Scheme, target.Host)
	}
	if target.PathPrefix != "/api" {
		t.Errorf("unexpected path prefix: %q", target.PathPrefix)
	}
	if target.APIKeyHeader != "Authorization" || target.APIKey != "Bearer sk-or-test" {
		t.Errorf("unexpected api key header/value: %s %s", target.APIKeyHeader, target.APIKey)
	}
	if target.Model != "anthropic/claude-haiku-4.5" {
		t.Errorf("unexpected model: %s", target.Model)
	}
}

func TestResolveDeepSeekPriorityOverOpenRouter(t *testing.T) {
	c := testConfig()
	c.OpenRouter = &OpenRouterConfig{APIKey: "sk-or-test", BaseURL: "https://openrouter.ai/api", Model: "anthropic/claude-haiku-4.5", Match: "haiku"}
	target := c.Resolve("/v1/messages", "claude-haiku-4-5-20251001")
	if target == nil || target.Host != "api.deepseek.com" {
		t.Errorf("expected DeepSeek to take priority, got %+v", target)
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

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	want := testConfig()
	want.SystemInject = &SystemInjectConfig{Match: "haiku", Text: "ask if unsure"}

	if err := Save(path, want); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if got.DeepSeek == nil || *got.DeepSeek != *want.DeepSeek {
		t.Errorf("DeepSeek = %+v, want %+v", got.DeepSeek, want.DeepSeek)
	}
	if got.SystemInject == nil || *got.SystemInject != *want.SystemInject {
		t.Errorf("SystemInject = %+v, want %+v", got.SystemInject, want.SystemInject)
	}
}

func TestSaveClearsEmptySections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := &Config{DeepSeek: &DeepSeekConfig{}, SystemInject: &SystemInjectConfig{}}

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if got.DeepSeek != nil {
		t.Errorf("DeepSeek = %+v, want nil", got.DeepSeek)
	}
	if got.SystemInject != nil {
		t.Errorf("SystemInject = %+v, want nil", got.SystemInject)
	}
}

func TestHolderGetSetAndSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	h, err := NewHolder(path)
	if err != nil {
		t.Fatalf("NewHolder error: %v", err)
	}
	if h.Get().DeepSeek != nil {
		t.Errorf("expected empty initial config, got %+v", h.Get())
	}

	cfg := testConfig()
	if err := h.SetAndSave(path, cfg); err != nil {
		t.Fatalf("SetAndSave error: %v", err)
	}
	if h.Get().DeepSeek == nil || *h.Get().DeepSeek != *cfg.DeepSeek {
		t.Errorf("Get() after SetAndSave = %+v, want %+v", h.Get().DeepSeek, cfg.DeepSeek)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if reloaded.DeepSeek == nil || *reloaded.DeepSeek != *cfg.DeepSeek {
		t.Errorf("reloaded config = %+v, want %+v", reloaded.DeepSeek, cfg.DeepSeek)
	}
}

func TestTestDeepSeekSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "sk-test" {
			t.Errorf("missing or wrong x-api-key header: %q", r.Header.Get("x-api-key"))
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"msg_1"}`))
	}))
	defer srv.Close()

	ok, msg := TestDeepSeek(&DeepSeekConfig{APIKey: "sk-test", BaseURL: srv.URL, Model: "deepseek-v4-pro"})
	if !ok {
		t.Errorf("expected ok=true, got message %q", msg)
	}
}

func TestTestDeepSeekInvalidKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	defer srv.Close()

	ok, msg := TestDeepSeek(&DeepSeekConfig{APIKey: "sk-bad", BaseURL: srv.URL})
	if ok {
		t.Errorf("expected ok=false, got ok=true with message %q", msg)
	}
}

func TestTestDeepSeekMissingFields(t *testing.T) {
	ok, _ := TestDeepSeek(&DeepSeekConfig{})
	if ok {
		t.Error("expected ok=false for empty config")
	}
}

func TestTestOpenRouterSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-or-test" {
			t.Errorf("missing or wrong Authorization header: %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"msg_1"}`))
	}))
	defer srv.Close()

	ok, msg := TestOpenRouter(&OpenRouterConfig{APIKey: "sk-or-test", BaseURL: srv.URL, Model: "anthropic/claude-haiku-4.5"})
	if !ok {
		t.Errorf("expected ok=true, got message %q", msg)
	}
}

func TestTestOpenRouterInvalidKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	defer srv.Close()

	ok, msg := TestOpenRouter(&OpenRouterConfig{APIKey: "sk-or-bad", BaseURL: srv.URL, Model: "anthropic/claude-haiku-4.5"})
	if ok {
		t.Errorf("expected ok=false, got ok=true with message %q", msg)
	}
}

func TestTestOpenRouterMissingFields(t *testing.T) {
	ok, _ := TestOpenRouter(&OpenRouterConfig{})
	if ok {
		t.Error("expected ok=false for empty config")
	}
}

func TestTestOpenRouterMissingModel(t *testing.T) {
	ok, msg := TestOpenRouter(&OpenRouterConfig{APIKey: "sk-or-test", BaseURL: "https://openrouter.ai/api"})
	if ok {
		t.Error("expected ok=false when model is missing")
	}
	if msg == "" {
		t.Error("expected a message explaining the missing model")
	}
}
