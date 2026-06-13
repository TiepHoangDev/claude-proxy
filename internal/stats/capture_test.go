package stats

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// TestCaptureWriterStreamingToolUse drives a CaptureWriter through a
// synthetic SSE stream containing a tool_use content block whose input is
// split across multiple input_json_delta events and multiple Write calls,
// and verifies usage, cache tokens, and the extracted tool_use are correct.
func TestCaptureWriterStreamingToolUse(t *testing.T) {
	const sse = `event: message_start
data: {"type":"message_start","message":{"model":"claude-sonnet-4-6","usage":{"input_tokens":10,"cache_creation_input_tokens":5,"cache_read_input_tokens":100}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_01","name":"Read","input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"file_path\": \"/tmp/foo.go\""}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":42}}

event: message_stop
data: {"type":"message_stop"}

`

	rec := httptest.NewRecorder()
	cw := NewCaptureWriter(rec, 128*1024)
	rec.Header().Set("Content-Type", "text/event-stream")
	cw.WriteHeader(200)

	// Write the SSE body in small, arbitrary chunks to exercise the
	// line-buffering logic across Write boundaries.
	const chunkSize = 17
	body := []byte(sse)
	for i := 0; i < len(body); i += chunkSize {
		end := i + chunkSize
		if end > len(body) {
			end = len(body)
		}
		if _, err := cw.Write(body[i:end]); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	result := cw.Finalize()

	if !result.Streaming {
		t.Errorf("Streaming = false, want true")
	}
	if result.InputTokens != 10 {
		t.Errorf("InputTokens = %d, want 10", result.InputTokens)
	}
	if result.CacheCreationInputTokens != 5 {
		t.Errorf("CacheCreationInputTokens = %d, want 5", result.CacheCreationInputTokens)
	}
	if result.CacheReadInputTokens != 100 {
		t.Errorf("CacheReadInputTokens = %d, want 100", result.CacheReadInputTokens)
	}
	if result.OutputTokens != 42 {
		t.Errorf("OutputTokens = %d, want 42", result.OutputTokens)
	}

	if len(result.ToolUses) != 1 {
		t.Fatalf("got %d tool uses, want 1 (%+v)", len(result.ToolUses), result.ToolUses)
	}
	tu := result.ToolUses[0]
	if tu.ID != "toolu_01" || tu.Name != "Read" {
		t.Errorf("tool use = %+v, want {ID: toolu_01, Name: Read}", tu)
	}
	var input struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(tu.Input, &input); err != nil {
		t.Fatalf("unmarshal tool use input %q: %v", tu.Input, err)
	}
	if input.FilePath != "/tmp/foo.go" {
		t.Errorf("input.FilePath = %q, want /tmp/foo.go", input.FilePath)
	}
}

// TestCaptureWriterStreamingResponseBlocks verifies that text and tool_use
// content blocks are reconstructed into ResponseBlocks in their original
// order, and that empty text blocks are omitted.
func TestCaptureWriterStreamingResponseBlocks(t *testing.T) {
	const sse = `event: message_start
data: {"type":"message_start","message":{"model":"claude-sonnet-4-6","usage":{"input_tokens":10}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":", world"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_01","name":"Read","input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"file_path\": \"/tmp/foo.go\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

event: content_block_start
data: {"type":"content_block_start","index":2,"content_block":{"type":"text","text":""}}

event: content_block_stop
data: {"type":"content_block_stop","index":2}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":20}}

event: message_stop
data: {"type":"message_stop"}

`

	rec := httptest.NewRecorder()
	cw := NewCaptureWriter(rec, 128*1024)
	rec.Header().Set("Content-Type", "text/event-stream")
	cw.WriteHeader(200)
	if _, err := cw.Write([]byte(sse)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	result := cw.Finalize()
	if len(result.ResponseBlocks) != 2 {
		t.Fatalf("got %d response blocks, want 2 (%+v)", len(result.ResponseBlocks), result.ResponseBlocks)
	}

	text := result.ResponseBlocks[0]
	if text.Type != "text" || text.Text != "Hello, world" {
		t.Errorf("block 0 = %+v, want {Type: text, Text: \"Hello, world\"}", text)
	}

	tool := result.ResponseBlocks[1]
	if tool.Type != "tool_use" || tool.Name != "Read" || tool.ToolUseID != "toolu_01" {
		t.Errorf("block 1 = %+v, want {Type: tool_use, Name: Read, ToolUseID: toolu_01}", tool)
	}
	var input struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(tool.Input, &input); err != nil {
		t.Fatalf("unmarshal tool input %q: %v", tool.Input, err)
	}
	if input.FilePath != "/tmp/foo.go" {
		t.Errorf("input.FilePath = %q, want /tmp/foo.go", input.FilePath)
	}
}

// TestCaptureWriterNonStreamingToolUse verifies tool_use blocks are
// extracted from a buffered non-streaming JSON response.
func TestCaptureWriterNonStreamingToolUse(t *testing.T) {
	const body = `{"model":"claude-sonnet-4-6","content":[{"type":"text","text":"ok"},{"type":"tool_use","id":"toolu_99","name":"Bash","input":{"command":"ls"}}],"usage":{"input_tokens":3,"output_tokens":7,"cache_read_input_tokens":50}}`

	rec := httptest.NewRecorder()
	cw := NewCaptureWriter(rec, 128*1024)
	rec.Header().Set("Content-Type", "application/json")
	cw.WriteHeader(200)
	if _, err := cw.Write([]byte(body)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	result := cw.Finalize()
	if result.Streaming {
		t.Errorf("Streaming = true, want false")
	}
	if result.InputTokens != 3 || result.OutputTokens != 7 || result.CacheReadInputTokens != 50 {
		t.Errorf("usage = %+v, want input=3 output=7 cacheRead=50", result)
	}
	if len(result.ToolUses) != 1 || result.ToolUses[0].Name != "Bash" {
		t.Fatalf("ToolUses = %+v, want [Bash]", result.ToolUses)
	}
}
