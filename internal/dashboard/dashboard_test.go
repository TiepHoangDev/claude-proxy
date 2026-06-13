package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"claude-proxy/internal/stats"
)

// TestDetailAPIHandlerTimeline verifies the detail API returns a
// chronological timeline covering the system prompt, prior conversation
// turns (including tool_use/tool_result blocks), and the new assistant
// response.
func TestDetailAPIHandlerTimeline(t *testing.T) {
	requestBody := `{
		"model": "claude-sonnet-4-6",
		"system": "You are a helpful assistant.",
		"messages": [
			{"role": "user", "content": "list the files"},
			{"role": "assistant", "content": [{"type": "tool_use", "id": "toolu_01", "name": "Bash", "input": {"command": "ls"}}]},
			{"role": "user", "content": [{"type": "tool_result", "tool_use_id": "toolu_01", "content": "a.go\nb.go"}]}
		]
	}`

	store := stats.NewStore(10)
	store.Add(stats.RequestLog{
		Timestamp:   time.Now(),
		Method:      "POST",
		Path:        "/v1/messages",
		Model:       "claude-sonnet-4-6",
		Status:      200,
		RequestBody: requestBody,
		ResponseBlocks: []stats.TimelineBlock{
			{Type: "text", Text: "Here are the files."},
			{Type: "tool_use", Name: "Read", ToolUseID: "toolu_02", Input: json.RawMessage(`{"file_path": "a.go"}`)},
		},
		ResponseBody: `{"role": "assistant", "content": [{"type": "text", "text": "Here are the files."}]}`,
	})

	req := httptest.NewRequest("GET", "/_proxy/api/requests/1", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	DetailAPIHandler(store)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp struct {
		Timeline []stats.TimelineEntry
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp.Timeline) != 5 {
		t.Fatalf("got %d timeline entries, want 5: %+v", len(resp.Timeline), resp.Timeline)
	}

	if resp.Timeline[0].Role != "system" || resp.Timeline[0].Blocks[0].Text != "You are a helpful assistant." {
		t.Errorf("entry 0 = %+v, want system prompt", resp.Timeline[0])
	}
	if resp.Timeline[1].Role != "user" || resp.Timeline[1].Blocks[0].Text != "list the files" {
		t.Errorf("entry 1 = %+v, want user 'list the files'", resp.Timeline[1])
	}
	if resp.Timeline[2].Role != "assistant" || resp.Timeline[2].Blocks[0].Type != "tool_use" ||
		resp.Timeline[2].Blocks[0].Name != "Bash" || resp.Timeline[2].Blocks[0].ToolUseID != "toolu_01" {
		t.Errorf("entry 2 = %+v, want assistant tool_use Bash/toolu_01", resp.Timeline[2])
	}
	if resp.Timeline[3].Role != "user" || resp.Timeline[3].Blocks[0].Type != "tool_result" ||
		resp.Timeline[3].Blocks[0].ToolUseID != "toolu_01" || resp.Timeline[3].Blocks[0].ResultText != "a.go\nb.go" {
		t.Errorf("entry 3 = %+v, want user tool_result for toolu_01 with content a.go\\nb.go", resp.Timeline[3])
	}
	if resp.Timeline[4].Role != "assistant" || resp.Timeline[4].Blocks[0].Type != "text" ||
		resp.Timeline[4].Blocks[0].Text != "Here are the files." || resp.Timeline[4].Blocks[1].Name != "Read" {
		t.Errorf("entry 4 = %+v, want assistant response with text + tool_use Read", resp.Timeline[4])
	}
}

// TestDetailAPIHandlerUnparsableRequest verifies the detail API returns an
// empty timeline (no error) when the request body isn't a Messages API
// request.
func TestDetailAPIHandlerUnparsableRequest(t *testing.T) {
	store := stats.NewStore(10)
	store.Add(stats.RequestLog{
		Timestamp:    time.Now(),
		Method:       "GET",
		Path:         "/v1/models",
		RequestBody:  "",
		ResponseBody: `{"data": []}`,
	})

	req := httptest.NewRequest("GET", "/_proxy/api/requests/1", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	DetailAPIHandler(store)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp struct {
		Timeline []stats.TimelineEntry
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Timeline) != 0 {
		t.Errorf("got %d timeline entries, want 0: %+v", len(resp.Timeline), resp.Timeline)
	}
}

// TestDetailAPIHandlerNotFound verifies unknown IDs return 404.
func TestDetailAPIHandlerNotFound(t *testing.T) {
	store := stats.NewStore(10)

	req := httptest.NewRequest("GET", "/_proxy/api/requests/99", nil)
	req.SetPathValue("id", "99")
	rec := httptest.NewRecorder()

	DetailAPIHandler(store)(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
