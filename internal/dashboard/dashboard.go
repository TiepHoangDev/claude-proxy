package dashboard

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"claude-proxy/internal/stats"
)

//go:embed static/index.html
var indexHTML []byte

//go:embed static/detail.html
var detailHTML []byte

// Handler serves the static dashboard page. Data is fetched client-side
// from APIHandler via Alpine.js.
func Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	}
}

// DetailHandler serves the static request-detail page. Data is fetched
// client-side from DetailAPIHandler via Alpine.js.
func DetailHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(detailHTML)
	}
}

// listItem adds presentation fields to stats.RequestLog for the dashboard
// table, computed once server-side so the client doesn't need to
// reimplement message-parsing logic.
type listItem struct {
	stats.RequestLog
	UserPreview string
	ToolNames   string
}

// userPreview returns a single-line, truncated preview of the most recent
// user message in a request body, for display in the dashboard table.
func userPreview(requestBody string) string {
	text := strings.Join(strings.Fields(stats.LastUserText(requestBody)), " ")
	const maxLen = 80
	r := []rune(text)
	if len(r) > maxLen {
		return string(r[:maxLen]) + "…"
	}
	return text
}

// APIHandler returns recent requests and totals as JSON.
func APIHandler(store *stats.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		recent := store.Recent()
		items := make([]listItem, len(recent))
		for i, item := range recent {
			items[i] = listItem{
				RequestLog:  item,
				UserPreview: userPreview(item.RequestBody),
				ToolNames:   strings.Join(stats.ToolNames(item.ToolUses), ", "),
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Totals stats.Totals `json:"totals"`
			Items  []listItem   `json:"items"`
		}{
			Totals: store.Totals(),
			Items:  items,
		})
	}
}

// DetailAPIHandler returns a single request, including its conversation
// timeline, as JSON.
func DetailAPIHandler(store *stats.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		item, ok := store.Get(id)
		if !ok {
			http.NotFound(w, r)
			return
		}

		timeline := stats.BuildRequestTimeline(item.RequestBody)
		if len(item.ResponseBlocks) > 0 {
			timeline = append(timeline, stats.TimelineEntry{Role: "assistant", Blocks: item.ResponseBlocks})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			stats.RequestLog
			Timeline []stats.TimelineEntry
		}{item, timeline})
	}
}
