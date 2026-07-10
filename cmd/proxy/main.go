package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strconv"
	"time"

	"claude-proxy/internal/dashboard"
	"claude-proxy/internal/router"
	"claude-proxy/internal/setup"
	"claude-proxy/internal/stats"
)

// routeCtxKey is the context key used to pass a resolved router.Target
// from statsMiddleware to the proxy's Director.
type routeCtxKey struct{}

const configPath = "config.json"

func main() {
	if err := os.MkdirAll("build", 0o755); err != nil {
		log.Fatalf("failed to create build directory: %v", err)
	}

	errFile, err := os.OpenFile("build/error.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		log.Fatalf("failed to open build/error.log: %v", err)
	}
	defer errFile.Close()
	log.SetOutput(io.MultiWriter(os.Stdout, errFile))

	target, err := url.Parse("https://api.anthropic.com")
	if err != nil {
		log.Fatalf("invalid target URL: %v", err)
	}

	routerHolder, err := router.NewHolder(configPath)
	if err != nil {
		log.Printf("failed to load %s, alternate routing disabled: %v", configPath, err)
		routerHolder = &router.Holder{}
	}

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			if t, ok := req.Context().Value(routeCtxKey{}).(*router.Target); ok && t != nil {
				req.URL.Scheme = t.Scheme
				req.URL.Host = t.Host
				req.URL.Path = path.Join(t.PathPrefix, req.URL.Path)
				req.Host = t.Host
				req.Header.Del("Authorization")
				req.Header.Set(t.APIKeyHeader, t.APIKey)
			} else {
				req.URL.Scheme = target.Scheme
				req.URL.Host = target.Host
				req.Host = target.Host
			}
			// Drop client's Accept-Encoding so Transport negotiates gzip itself
			// and transparently decompresses the response, giving us plain-text
			// bodies to parse for token usage.
			req.Header.Del("Accept-Encoding")
		},
		// Negative FlushInterval streams response bodies immediately, required for SSE.
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("proxy error for %s %s: %v", r.Method, r.URL.Path, err)
			w.WriteHeader(http.StatusBadGateway)
		},
	}

	store := stats.NewStore(200)
	reqLogger := stats.NewFileLogger("build/request.log", 100)
	toolLogger := stats.NewFileLogger("build/tools.log", 500)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	go func() {
		for {
			cfg := routerHolder.Get()
			if cfg != nil && cfg.DeepSeek != nil {
				bal := router.FetchDeepSeekBalance(cfg.DeepSeek)
				if bal != nil {
					store.UpdateDeepSeekBalance(&stats.DeepSeekBalance{
						IsAvailable: bal.IsAvailable,
						Currency:    bal.Currency,
						Total:       bal.Total,
						Display:     bal.Display(),
					})
				}
			}
			if usage := fetchClaudeUsage(); usage != nil {
				store.UpdateClaudeUsage(usage)
			}
			store.UpdateActiveRoute(computeActiveRoute(cfg))
			time.Sleep(60 * time.Second)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/_proxy/dashboard", dashboard.Handler())
	mux.HandleFunc("/_proxy/api/requests", dashboard.APIHandler(store))
	mux.HandleFunc("/_proxy/api/requests/{id}", dashboard.DetailAPIHandler(store))
	mux.HandleFunc("/_proxy/requests/{id}", dashboard.DetailHandler())
	mux.HandleFunc("/_proxy/setup", setup.Handler())
	mux.HandleFunc("/_proxy/api/setup/status", setup.StatusAPIHandler(routerHolder, port))
	mux.HandleFunc("/_proxy/api/setup/save", setup.SaveAPIHandler(routerHolder))
	mux.HandleFunc("/_proxy/api/setup/test-deepseek", setup.TestDeepSeekAPIHandler())
	mux.HandleFunc("/_proxy/api/setup/test-openrouter", setup.TestOpenRouterAPIHandler())
	mux.HandleFunc("/_proxy/api/setup/models", setup.ModelsAPIHandler())
	mux.HandleFunc("/_proxy/api/health", healthAPIHandler(store))
	mux.HandleFunc("/", statsMiddleware(store, reqLogger, toolLogger, routerHolder, proxy))

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 30 * time.Second,
	}

	dashboardURL := fmt.Sprintf("http://localhost:%s/_proxy/dashboard", port)
	log.Printf("proxying to %s on :%s, dashboard at %s", target, port, dashboardURL)

	if os.Getenv("NO_BROWSER") == "" {
		startURL := dashboardURL
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			startURL = fmt.Sprintf("http://localhost:%s/_proxy/setup", port)
		}
		go func() {
			time.Sleep(300 * time.Millisecond)
			openBrowser(startURL)
		}()
	}

	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

const (
	// captureBodyCap is how much of the response body CaptureWriter buffers
	// for usage/tool-call extraction and raw display.
	captureBodyCap = 128 * 1024
	// maxStoredBody is how much of each request/response body is retained
	// in memory for the request detail page.
	maxStoredBody = 32 * 1024
)

func statsMiddleware(store *stats.Store, reqLogger, toolLogger *stats.FileLogger, routerHolder *router.Holder, proxy *httputil.ReverseProxy) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		routerCfg := routerHolder.Get()

		var bodyBytes []byte
		var reqModel string
		var toolResults []stats.ToolResult
		if r.Body != nil {
			var err error
			bodyBytes, err = io.ReadAll(r.Body)
			if err == nil {
				r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				reqModel = stats.ExtractRequestModel(bodyBytes)
				toolResults = stats.ExtractToolResults(bodyBytes)
			}
		}

		if auth := r.Header.Get("Authorization"); auth != "" {
			store.SetAuth(auth)
		} else if key := r.Header.Get("x-api-key"); key != "" {
			store.SetAuth(key)
		}

		if text, ok := routerCfg.InjectSystem(reqModel); ok {
			if newBody, err := router.InjectSystemPrompt(bodyBytes, text); err == nil {
				bodyBytes = newBody
				r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				r.ContentLength = int64(len(bodyBytes))
				r.Header.Set("Content-Length", strconv.Itoa(len(bodyBytes)))
			} else {
				log.Printf("failed to inject system prompt for %s: %v", r.URL.Path, err)
			}
		}

		if target := routerCfg.Resolve(r.URL.Path, reqModel); target != nil {
			if newBody, err := router.RewriteModel(bodyBytes, target.Model); err == nil {
				bodyBytes = newBody
				r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				r.ContentLength = int64(len(bodyBytes))
				r.Header.Set("Content-Length", strconv.Itoa(len(bodyBytes)))
			} else {
				log.Printf("failed to rewrite model for %s: %v", r.URL.Path, err)
			}
			r = r.WithContext(context.WithValue(r.Context(), routeCtxKey{}, target))
		}

		cw := stats.NewCaptureWriter(w, captureBodyCap)
		proxy.ServeHTTP(cw, r)

		result := cw.Finalize()
		model := result.Model
		if model == "" {
			model = reqModel
		}

		// Update rate limit and API key health state from the response.
		apiKeyErr := result.APIKeyError
		if apiKeyErr == "" && cw.Status() == 403 {
			apiKeyErr = "Forbidden: check if your API key has funds or access"
		}
		store.UpdateHealth(result.RateLimit, apiKeyErr)

		entry := stats.RequestLog{
			Timestamp:                start,
			Method:                   r.Method,
			Path:                     r.URL.Path,
			Model:                    model,
			Status:                   cw.Status(),
			InputTokens:              result.InputTokens,
			OutputTokens:             result.OutputTokens,
			CacheCreationInputTokens: result.CacheCreationInputTokens,
			CacheReadInputTokens:     result.CacheReadInputTokens,
			TotalInputTokens:         result.InputTokens + result.CacheCreationInputTokens + result.CacheReadInputTokens,
			DurationMs:               time.Since(start).Milliseconds(),
			Streaming:                result.Streaming,
			ToolUses:                 result.ToolUses,
			ToolResults:              toolResults,
			ResponseBlocks:           result.ResponseBlocks,
			RequestBody:              capBody(bodyBytes),
			ResponseBody:             capBody(cw.Body()),
		}
		store.Add(entry)
		if err := reqLogger.Append(entry); err != nil {
			log.Printf("failed to write build/request.log: %v", err)
		}

		for _, tu := range result.ToolUses {
			if err := toolLogger.Append(stats.ToolLogEntry{
				Type:      "tool_use",
				Timestamp: start,
				Name:      tu.Name,
				Input:     tu.Input,
			}); err != nil {
				log.Printf("failed to write build/tools.log: %v", err)
			}
		}
		for _, tr := range toolResults {
			if err := toolLogger.Append(stats.ToolLogEntry{
				Type:      "tool_result",
				Timestamp: start,
				ToolUseID: tr.ToolUseID,
				Size:      tr.Size,
			}); err != nil {
				log.Printf("failed to write build/tools.log: %v", err)
			}
		}
	}
}

// capBody truncates b to maxStoredBody bytes for in-memory retention,
// appending a marker if truncated. If b is valid JSON, it is pretty-printed
// first so that truncation (if any) still leaves readable, indented JSON.
func capBody(b []byte) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, b, "", "  "); err == nil {
		b = buf.Bytes()
	}
	if len(b) <= maxStoredBody {
		return string(b)
	}
	return string(b[:maxStoredBody]) + "\n...(truncated)"
}

// computeActiveRoute reports which alternate provider (if any) requests are
// currently being routed to (DeepSeek takes priority over OpenRouter,
// matching router.Config.Resolve), along with that model's pricing looked up
// from OpenRouter's public catalog — which also covers DeepSeek models,
// since DeepSeek doesn't expose pricing via an API.
func computeActiveRoute(cfg *router.Config) *stats.ActiveRoute {
	if cfg == nil {
		return nil
	}
	if cfg.DeepSeek != nil && cfg.DeepSeek.Model != "" {
		route := &stats.ActiveRoute{Provider: "deepseek", Model: cfg.DeepSeek.Model}
		if models, err := router.FetchOpenRouterModels(""); err == nil {
			if m := findOpenRouterModel(models, "deepseek/"+cfg.DeepSeek.Model); m != nil {
				route.PromptPricePerM = m.PromptPricePerM
				route.CompletionPricePerM = m.CompletionPricePerM
			}
		}
		return route
	}
	if cfg.OpenRouter != nil && cfg.OpenRouter.Model != "" {
		route := &stats.ActiveRoute{Provider: "openrouter", Model: cfg.OpenRouter.Model}
		if models, err := router.FetchOpenRouterModels(cfg.OpenRouter.BaseURL); err == nil {
			if m := findOpenRouterModel(models, cfg.OpenRouter.Model); m != nil {
				route.PromptPricePerM = m.PromptPricePerM
				route.CompletionPricePerM = m.CompletionPricePerM
			}
		}
		return route
	}
	return nil
}

func findOpenRouterModel(models []router.OpenRouterModel, id string) *router.OpenRouterModel {
	for i := range models {
		if models[i].ID == id {
			return &models[i]
		}
	}
	return nil
}

func fetchClaudeUsage() *stats.ClaudeUsage {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	credsPath := home + "/.claude/.credentials.json"
	data, err := os.ReadFile(credsPath)
	if err != nil {
		return nil
	}
	var creds struct {
		ClaudeAiOauth struct {
			AccessToken string `json:"accessToken"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(data, &creds); err != nil || creds.ClaudeAiOauth.AccessToken == "" {
		return nil
	}

	req, _ := http.NewRequest(http.MethodGet, "https://api.anthropic.com/api/oauth/usage", nil)
	req.Header.Set("Authorization", "Bearer "+creds.ClaudeAiOauth.AccessToken)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var result struct {
		FiveHour struct {
			Utilization float64 `json:"utilization"`
			ResetsAt    string  `json:"resets_at"`
		} `json:"five_hour"`
		SevenDay struct {
			Utilization float64 `json:"utilization"`
			ResetsAt    string  `json:"resets_at"`
		} `json:"seven_day"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil
	}

	return &stats.ClaudeUsage{
		FiveHourPercent: result.FiveHour.Utilization,
		FiveHourReset:   result.FiveHour.ResetsAt,
		SevenDayPercent: result.SevenDay.Utilization,
		SevenDayReset:   result.SevenDay.ResetsAt,
	}
}

func healthAPIHandler(store *stats.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(store.Health())
	}
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("could not open browser: %v", err)
	}
}
