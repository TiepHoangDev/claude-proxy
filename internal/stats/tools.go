package stats

import (
	"encoding/json"
	"strings"
	"time"
)

// ToolUse represents a "tool_use" content block emitted by the model.
type ToolUse struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input,omitempty"`
}

// ToolResult represents a "tool_result" content block sent by the client.
type ToolResult struct {
	ToolUseID string `json:"tool_use_id"`
	IsError   bool   `json:"is_error,omitempty"`
	Size      int    `json:"size"`
}

// ToolLogEntry is a single line written to tools.log, covering both
// tool_use and tool_result events.
type ToolLogEntry struct {
	Type      string          `json:"type"` // "tool_use" or "tool_result"
	Timestamp time.Time       `json:"timestamp"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Size      int             `json:"size,omitempty"`
}

// contentBlock covers the union of fields used by text, tool_use,
// tool_result, thinking, and image content blocks in the Anthropic
// Messages API.
type contentBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Source    json.RawMessage `json:"source,omitempty"`
}

// TimelineBlock is a single content block rendered in the conversation
// timeline on the request detail page.
type TimelineBlock struct {
	Type       string // "text", "tool_use", "tool_result", "thinking", "image", "unknown"
	Text       string
	Name       string          // tool_use
	ToolUseID  string          // tool_use / tool_result
	Input      json.RawMessage // tool_use
	IsError    bool            // tool_result
	ResultText string          // tool_result content flattened to text (best effort)
	Raw        string          // fallback: raw JSON for unrecognized block types
}

// TimelineEntry is one turn (system prompt, or a message with a role) in
// the conversation timeline.
type TimelineEntry struct {
	Role   string // "system", "user", "assistant"
	Blocks []TimelineBlock
}

// blockToTimeline converts a single Anthropic content block into a
// TimelineBlock. Unrecognized types fall back to their raw JSON.
func blockToTimeline(b contentBlock, raw json.RawMessage) TimelineBlock {
	switch b.Type {
	case "text":
		return TimelineBlock{Type: "text", Text: b.Text}
	case "thinking":
		return TimelineBlock{Type: "thinking", Text: b.Thinking}
	case "tool_use":
		return TimelineBlock{Type: "tool_use", Name: b.Name, ToolUseID: b.ID, Input: b.Input}
	case "tool_result":
		return TimelineBlock{
			Type:       "tool_result",
			ToolUseID:  b.ToolUseID,
			IsError:    b.IsError,
			ResultText: flattenToolResultContent(b.Content),
		}
	case "image":
		return TimelineBlock{Type: "image"}
	default:
		return TimelineBlock{Type: "unknown", Raw: string(raw)}
	}
}

// flattenToolResultContent turns a tool_result block's "content" field
// (a string, or an array of content blocks) into plain text for display.
func flattenToolResultContent(content json.RawMessage) string {
	if len(content) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(content, &s); err == nil {
		return s
	}
	var blocks []contentBlock
	if err := json.Unmarshal(content, &blocks); err == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	return string(content)
}

// blocksFromContent converts a message's "content" field (a string, or an
// array of content blocks) into a slice of TimelineBlock.
func blocksFromContent(content json.RawMessage) []TimelineBlock {
	if len(content) == 0 {
		return nil
	}
	var s string
	if err := json.Unmarshal(content, &s); err == nil {
		if s == "" {
			return nil
		}
		return []TimelineBlock{{Type: "text", Text: s}}
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(content, &raws); err != nil {
		return nil
	}
	out := make([]TimelineBlock, 0, len(raws))
	for _, raw := range raws {
		var b contentBlock
		if err := json.Unmarshal(raw, &b); err != nil {
			out = append(out, TimelineBlock{Type: "unknown", Raw: string(raw)})
			continue
		}
		out = append(out, blockToTimeline(b, raw))
	}
	return out
}

// BlocksFromResponse converts a non-streaming Messages API response body's
// top-level "content" array into TimelineBlocks, in order.
func BlocksFromResponse(body []byte) []TimelineBlock {
	var resp struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil
	}
	return blocksFromContent(resp.Content)
}

// LastUserText returns the concatenated text blocks of the most recent
// "user" message that contains text, for use as a preview in the
// dashboard. User turns that are purely tool_result (no text, as in
// agentic tool-use loops) are skipped in favor of an earlier turn.
// Returns "" if the body can't be parsed or no user message has text.
func LastUserText(requestBody string) string {
	timeline := BuildRequestTimeline(requestBody)
	for i := len(timeline) - 1; i >= 0; i-- {
		if timeline[i].Role != "user" {
			continue
		}
		var parts []string
		for _, b := range timeline[i].Blocks {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	return ""
}

// BuildRequestTimeline parses a Messages API request body into a
// chronological sequence of timeline entries: an optional "system" entry,
// followed by one entry per message in "messages". Returns nil if the body
// cannot be parsed as a Messages API request.
func BuildRequestTimeline(requestBody string) []TimelineEntry {
	var req struct {
		System   json.RawMessage `json:"system"`
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(requestBody), &req); err != nil {
		return nil
	}

	var out []TimelineEntry
	if blocks := blocksFromContent(req.System); len(blocks) > 0 {
		out = append(out, TimelineEntry{Role: "system", Blocks: blocks})
	}
	for _, m := range req.Messages {
		out = append(out, TimelineEntry{Role: m.Role, Blocks: blocksFromContent(m.Content)})
	}
	return out
}

// ExtractToolUses scans a non-streaming Messages API response body for
// top-level "tool_use" content blocks.
func ExtractToolUses(body []byte) []ToolUse {
	var resp struct {
		Content []contentBlock `json:"content"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil
	}
	var out []ToolUse
	for _, b := range resp.Content {
		if b.Type == "tool_use" {
			out = append(out, ToolUse{ID: b.ID, Name: b.Name, Input: b.Input})
		}
	}
	return out
}

// ExtractToolResults scans a Messages API request body for "tool_result"
// content blocks across all messages.
func ExtractToolResults(body []byte) []ToolResult {
	var req struct {
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil
	}
	var out []ToolResult
	for _, m := range req.Messages {
		var blocks []contentBlock
		if err := json.Unmarshal(m.Content, &blocks); err != nil {
			continue // content is a plain string, no blocks
		}
		for _, b := range blocks {
			if b.Type == "tool_result" {
				out = append(out, ToolResult{
					ToolUseID: b.ToolUseID,
					IsError:   b.IsError,
					Size:      len(b.Content),
				})
			}
		}
	}
	return out
}

// ToolNames returns the tool names from a list of tool uses, in order.
func ToolNames(uses []ToolUse) []string {
	if len(uses) == 0 {
		return nil
	}
	names := make([]string, len(uses))
	for i, u := range uses {
		names[i] = u.Name
	}
	return names
}
