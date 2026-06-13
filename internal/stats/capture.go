package stats

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
)

type usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

type sseEventType struct {
	Type string `json:"type"`
}

type sseMessageStart struct {
	Message struct {
		Model string `json:"model"`
		Usage usage  `json:"usage"`
	} `json:"message"`
}

type sseMessageDelta struct {
	Usage usage `json:"usage"`
}

type sseContentBlockStart struct {
	Index        int `json:"index"`
	ContentBlock struct {
		Type  string          `json:"type"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	} `json:"content_block"`
}

type sseContentBlockDelta struct {
	Index int `json:"index"`
	Delta struct {
		Type        string `json:"type"`
		PartialJSON string `json:"partial_json"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
	} `json:"delta"`
}

type sseContentBlockStop struct {
	Index int `json:"index"`
}

type nonStreamResponse struct {
	Model string `json:"model"`
	Usage usage  `json:"usage"`
}

// ExtractRequestModel pulls the "model" field out of a JSON request body.
func ExtractRequestModel(body []byte) string {
	var req struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return ""
	}
	return req.Model
}

// UsageResult holds token usage, tool calls, and metadata extracted from a
// proxied response.
type UsageResult struct {
	Model                    string
	InputTokens              int
	OutputTokens             int
	CacheCreationInputTokens int
	CacheReadInputTokens     int
	Streaming                bool
	ToolUses                 []ToolUse
	ResponseBlocks           []TimelineBlock
}

// pendingBlock accumulates streamed deltas for a content block (tool_use
// "input_json_delta", text "text_delta", or thinking "thinking_delta")
// until its content_block_stop arrives.
type pendingBlock struct {
	blockType string // "tool_use", "text", "thinking"
	id, name  string
	initial   json.RawMessage // tool_use: initial (often empty) input object
	buf       bytes.Buffer
}

// CaptureWriter wraps an http.ResponseWriter, passing bytes through
// unmodified while incrementally extracting token usage and tool_use
// info so that streaming (SSE) responses don't need to be fully buffered.
type CaptureWriter struct {
	http.ResponseWriter
	status        int
	isSSE         bool
	lineBuf       []byte
	result        UsageResult
	bodyBuf       bytes.Buffer
	bodyCap       int
	pendingBlocks map[int]*pendingBlock
}

// NewCaptureWriter wraps w, buffering up to bodyCap bytes of the response
// body (for both streaming and non-streaming responses) for usage and
// tool-call extraction plus raw display.
func NewCaptureWriter(w http.ResponseWriter, bodyCap int) *CaptureWriter {
	return &CaptureWriter{
		ResponseWriter: w,
		status:         http.StatusOK,
		bodyCap:        bodyCap,
	}
}

// Status returns the response status code written, defaulting to 200.
func (c *CaptureWriter) Status() int {
	return c.status
}

// Body returns the captured response body, up to bodyCap bytes.
func (c *CaptureWriter) Body() []byte {
	return c.bodyBuf.Bytes()
}

func (c *CaptureWriter) WriteHeader(code int) {
	c.status = code
	ct := c.ResponseWriter.Header().Get("Content-Type")
	c.isSSE = strings.Contains(ct, "text/event-stream")
	c.result.Streaming = c.isSSE
	c.ResponseWriter.WriteHeader(code)
}

func (c *CaptureWriter) Write(b []byte) (int, error) {
	if c.isSSE {
		c.processSSE(b)
	}
	if c.bodyBuf.Len() < c.bodyCap {
		remain := c.bodyCap - c.bodyBuf.Len()
		if remain >= len(b) {
			c.bodyBuf.Write(b)
		} else {
			c.bodyBuf.Write(b[:remain])
		}
	}
	return c.ResponseWriter.Write(b)
}

func (c *CaptureWriter) Flush() {
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (c *CaptureWriter) processSSE(b []byte) {
	c.lineBuf = append(c.lineBuf, b...)
	for {
		idx := bytes.IndexByte(c.lineBuf, '\n')
		if idx < 0 {
			break
		}
		line := c.lineBuf[:idx]
		c.lineBuf = c.lineBuf[idx+1:]
		c.processLine(line)
	}
	if len(c.lineBuf) > 64*1024 {
		c.lineBuf = nil
	}
}

func (c *CaptureWriter) processLine(line []byte) {
	line = bytes.TrimSpace(line)
	data, ok := bytes.CutPrefix(line, []byte("data:"))
	if !ok {
		return
	}
	data = bytes.TrimSpace(data)

	var head sseEventType
	if err := json.Unmarshal(data, &head); err != nil {
		return
	}

	switch head.Type {
	case "message_start":
		var start sseMessageStart
		if err := json.Unmarshal(data, &start); err != nil {
			return
		}
		if start.Message.Usage.InputTokens > 0 {
			c.result.InputTokens = start.Message.Usage.InputTokens
		}
		c.result.CacheCreationInputTokens = start.Message.Usage.CacheCreationInputTokens
		c.result.CacheReadInputTokens = start.Message.Usage.CacheReadInputTokens
		if start.Message.Model != "" {
			c.result.Model = start.Message.Model
		}

	case "message_delta":
		var delta sseMessageDelta
		if err := json.Unmarshal(data, &delta); err != nil {
			return
		}
		if delta.Usage.OutputTokens > 0 {
			c.result.OutputTokens = delta.Usage.OutputTokens
		}
		if delta.Usage.CacheCreationInputTokens > 0 {
			c.result.CacheCreationInputTokens = delta.Usage.CacheCreationInputTokens
		}
		if delta.Usage.CacheReadInputTokens > 0 {
			c.result.CacheReadInputTokens = delta.Usage.CacheReadInputTokens
		}

	case "content_block_start":
		var start sseContentBlockStart
		if err := json.Unmarshal(data, &start); err != nil {
			return
		}
		switch start.ContentBlock.Type {
		case "tool_use", "text", "thinking":
			if c.pendingBlocks == nil {
				c.pendingBlocks = make(map[int]*pendingBlock)
			}
			c.pendingBlocks[start.Index] = &pendingBlock{
				blockType: start.ContentBlock.Type,
				id:        start.ContentBlock.ID,
				name:      start.ContentBlock.Name,
				initial:   start.ContentBlock.Input,
			}
		}

	case "content_block_delta":
		var delta sseContentBlockDelta
		if err := json.Unmarshal(data, &delta); err != nil {
			return
		}
		pb, ok := c.pendingBlocks[delta.Index]
		if !ok {
			return
		}
		switch delta.Delta.Type {
		case "input_json_delta":
			pb.buf.WriteString(delta.Delta.PartialJSON)
		case "text_delta":
			pb.buf.WriteString(delta.Delta.Text)
		case "thinking_delta":
			pb.buf.WriteString(delta.Delta.Thinking)
		}

	case "content_block_stop":
		var stop sseContentBlockStop
		if err := json.Unmarshal(data, &stop); err != nil {
			return
		}
		pb, ok := c.pendingBlocks[stop.Index]
		if !ok {
			return
		}
		delete(c.pendingBlocks, stop.Index)

		switch pb.blockType {
		case "tool_use":
			input := pb.initial
			if pb.buf.Len() > 0 {
				input = json.RawMessage(pb.buf.Bytes())
			}
			input = append(json.RawMessage{}, input...)
			c.result.ToolUses = append(c.result.ToolUses, ToolUse{ID: pb.id, Name: pb.name, Input: input})
			c.result.ResponseBlocks = append(c.result.ResponseBlocks, TimelineBlock{
				Type: "tool_use", Name: pb.name, ToolUseID: pb.id, Input: input,
			})
		case "text":
			if pb.buf.Len() > 0 {
				c.result.ResponseBlocks = append(c.result.ResponseBlocks, TimelineBlock{Type: "text", Text: pb.buf.String()})
			}
		case "thinking":
			if pb.buf.Len() > 0 {
				c.result.ResponseBlocks = append(c.result.ResponseBlocks, TimelineBlock{Type: "thinking", Text: pb.buf.String()})
			}
		}
	}
}

// Finalize returns the extracted usage and tool-call info, parsing the
// buffered non-streaming body if needed.
func (c *CaptureWriter) Finalize() UsageResult {
	if !c.isSSE && c.bodyBuf.Len() > 0 {
		var resp nonStreamResponse
		if err := json.Unmarshal(c.bodyBuf.Bytes(), &resp); err == nil {
			c.result.Model = resp.Model
			c.result.InputTokens = resp.Usage.InputTokens
			c.result.OutputTokens = resp.Usage.OutputTokens
			c.result.CacheCreationInputTokens = resp.Usage.CacheCreationInputTokens
			c.result.CacheReadInputTokens = resp.Usage.CacheReadInputTokens
		}
		c.result.ToolUses = ExtractToolUses(c.bodyBuf.Bytes())
		c.result.ResponseBlocks = BlocksFromResponse(c.bodyBuf.Bytes())
	}
	return c.result
}
