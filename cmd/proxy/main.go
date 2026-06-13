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
	"claude-proxy/internal/stats"
)

// routeCtxKey is the context key used to pass a resolved router.Target
// from statsMiddleware to the proxy's Director.
type routeCtxKey struct{}

func main() {
	errFile, err := os.OpenFile("error.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		log.Fatalf("failed to open error.log: %v", err)
	}
	defer errFile.Close()
	log.SetOutput(io.MultiWriter(os.Stdout, errFile))

	target, err := url.Parse("https://api.anthropic.com")
	if err != nil {
		log.Fatalf("invalid target URL: %v", err)
	}

	routerCfg, err := router.Load("config.json")
	if err != nil {
		log.Printf("failed to load config.json, alternate routing disabled: %v", err)
		routerCfg = &router.Config{}
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
	reqLogger := stats.NewFileLogger("request.log", 100)
	toolLogger := stats.NewFileLogger("tools.log", 500)

	mux := http.NewServeMux()
	mux.HandleFunc("/_proxy/dashboard", dashboard.Handler())
	mux.HandleFunc("/_proxy/api/requests", dashboard.APIHandler(store))
	mux.HandleFunc("/_proxy/api/requests/{id}", dashboard.DetailAPIHandler(store))
	mux.HandleFunc("/_proxy/requests/{id}", dashboard.DetailHandler())
	mux.HandleFunc("/", statsMiddleware(store, reqLogger, toolLogger, routerCfg, proxy))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 30 * time.Second,
	}

	dashboardURL := fmt.Sprintf("http://localhost:%s/_proxy/dashboard", port)
	log.Printf("proxying to %s on :%s, dashboard at %s", target, port, dashboardURL)

	if os.Getenv("NO_BROWSER") == "" {
		go func() {
			time.Sleep(300 * time.Millisecond)
			openBrowser(dashboardURL)
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

func statsMiddleware(store *stats.Store, reqLogger, toolLogger *stats.FileLogger, routerCfg *router.Config, proxy *httputil.ReverseProxy) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

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
			log.Printf("failed to write request.log: %v", err)
		}

		for _, tu := range result.ToolUses {
			if err := toolLogger.Append(stats.ToolLogEntry{
				Type:      "tool_use",
				Timestamp: start,
				Name:      tu.Name,
				Input:     tu.Input,
			}); err != nil {
				log.Printf("failed to write tools.log: %v", err)
			}
		}
		for _, tr := range toolResults {
			if err := toolLogger.Append(stats.ToolLogEntry{
				Type:      "tool_result",
				Timestamp: start,
				ToolUseID: tr.ToolUseID,
				Size:      tr.Size,
			}); err != nil {
				log.Printf("failed to write tools.log: %v", err)
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
