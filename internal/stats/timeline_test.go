package stats

import (
	"encoding/json"
	"testing"
)

func TestBlocksFromContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []TimelineBlock
	}{
		{
			name:    "plain string",
			content: `"hello there"`,
			want:    []TimelineBlock{{Type: "text", Text: "hello there"}},
		},
		{
			name:    "empty string",
			content: `""`,
			want:    nil,
		},
		{
			name:    "mixed blocks",
			content: `[{"type": "text", "text": "checking..."}, {"type": "tool_use", "id": "toolu_01", "name": "Read", "input": {"file_path": "/tmp/foo.go"}}]`,
			want: []TimelineBlock{
				{Type: "text", Text: "checking..."},
				{Type: "tool_use", Name: "Read", ToolUseID: "toolu_01", Input: json.RawMessage(`{"file_path": "/tmp/foo.go"}`)},
			},
		},
		{
			name:    "tool_result with string content",
			content: `[{"type": "tool_result", "tool_use_id": "toolu_01", "content": "file contents here"}]`,
			want: []TimelineBlock{
				{Type: "tool_result", ToolUseID: "toolu_01", ResultText: "file contents here"},
			},
		},
		{
			name:    "tool_result with error and block-array content",
			content: `[{"type": "tool_result", "tool_use_id": "toolu_02", "is_error": true, "content": [{"type": "text", "text": "boom"}]}]`,
			want: []TimelineBlock{
				{Type: "tool_result", ToolUseID: "toolu_02", IsError: true, ResultText: "boom"},
			},
		},
		{
			name:    "image block",
			content: `[{"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "abc"}}]`,
			want:    []TimelineBlock{{Type: "image"}},
		},
		{
			name:    "thinking block",
			content: `[{"type": "thinking", "thinking": "let me think..."}]`,
			want:    []TimelineBlock{{Type: "thinking", Text: "let me think..."}},
		},
		{
			name:    "empty content",
			content: ``,
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := blocksFromContent(json.RawMessage(tt.content))
			if len(got) != len(tt.want) {
				t.Fatalf("got %d blocks, want %d (%+v)", len(got), len(tt.want), got)
			}
			for i := range tt.want {
				g, w := got[i], tt.want[i]
				if g.Type != w.Type || g.Text != w.Text || g.Name != w.Name || g.ToolUseID != w.ToolUseID || g.IsError != w.IsError || g.ResultText != w.ResultText {
					t.Errorf("block %d = %+v, want %+v", i, g, w)
				}
				if !jsonEqual(g.Input, w.Input) {
					t.Errorf("block %d input = %s, want %s", i, g.Input, w.Input)
				}
			}
		})
	}
}

func TestBuildRequestTimeline(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []TimelineEntry
	}{
		{
			name: "system string and multi-turn messages",
			body: `{
				"system": "You are a helpful assistant.",
				"messages": [
					{"role": "user", "content": "list files"},
					{"role": "assistant", "content": [{"type": "tool_use", "id": "toolu_01", "name": "Bash", "input": {"command": "ls"}}]},
					{"role": "user", "content": [{"type": "tool_result", "tool_use_id": "toolu_01", "content": "a.go\nb.go"}]}
				]
			}`,
			want: []TimelineEntry{
				{Role: "system", Blocks: []TimelineBlock{{Type: "text", Text: "You are a helpful assistant."}}},
				{Role: "user", Blocks: []TimelineBlock{{Type: "text", Text: "list files"}}},
				{Role: "assistant", Blocks: []TimelineBlock{{Type: "tool_use", Name: "Bash", ToolUseID: "toolu_01", Input: json.RawMessage(`{"command": "ls"}`)}}},
				{Role: "user", Blocks: []TimelineBlock{{Type: "tool_result", ToolUseID: "toolu_01", ResultText: "a.go\nb.go"}}},
			},
		},
		{
			name: "system as content-block array",
			body: `{
				"system": [{"type": "text", "text": "be concise"}],
				"messages": [{"role": "user", "content": "hi"}]
			}`,
			want: []TimelineEntry{
				{Role: "system", Blocks: []TimelineBlock{{Type: "text", Text: "be concise"}}},
				{Role: "user", Blocks: []TimelineBlock{{Type: "text", Text: "hi"}}},
			},
		},
		{
			name: "no system prompt",
			body: `{"messages": [{"role": "user", "content": "hi"}]}`,
			want: []TimelineEntry{
				{Role: "user", Blocks: []TimelineBlock{{Type: "text", Text: "hi"}}},
			},
		},
		{
			name: "invalid json",
			body: `not json`,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildRequestTimeline(tt.body)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d entries, want %d (%+v)", len(got), len(tt.want), got)
			}
			for i := range tt.want {
				if got[i].Role != tt.want[i].Role {
					t.Errorf("entry %d role = %q, want %q", i, got[i].Role, tt.want[i].Role)
				}
				if len(got[i].Blocks) != len(tt.want[i].Blocks) {
					t.Fatalf("entry %d blocks = %+v, want %+v", i, got[i].Blocks, tt.want[i].Blocks)
				}
				for j := range tt.want[i].Blocks {
					g, w := got[i].Blocks[j], tt.want[i].Blocks[j]
					if g.Type != w.Type || g.Text != w.Text || g.Name != w.Name || g.ToolUseID != w.ToolUseID || g.ResultText != w.ResultText {
						t.Errorf("entry %d block %d = %+v, want %+v", i, j, g, w)
					}
					if !jsonEqual(g.Input, w.Input) {
						t.Errorf("entry %d block %d input = %s, want %s", i, j, g.Input, w.Input)
					}
				}
			}
		})
	}
}

func TestBlocksFromResponse(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []TimelineBlock
	}{
		{
			name: "text and tool_use",
			body: `{"content": [{"type": "text", "text": "Let me check."}, {"type": "tool_use", "id": "toolu_01", "name": "Read", "input": {"file_path": "/tmp/foo.go"}}]}`,
			want: []TimelineBlock{
				{Type: "text", Text: "Let me check."},
				{Type: "tool_use", Name: "Read", ToolUseID: "toolu_01", Input: json.RawMessage(`{"file_path": "/tmp/foo.go"}`)},
			},
		},
		{
			name: "invalid json",
			body: `not json`,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BlocksFromResponse([]byte(tt.body))
			if len(got) != len(tt.want) {
				t.Fatalf("got %d blocks, want %d (%+v)", len(got), len(tt.want), got)
			}
			for i := range tt.want {
				g, w := got[i], tt.want[i]
				if g.Type != w.Type || g.Text != w.Text || g.Name != w.Name || g.ToolUseID != w.ToolUseID {
					t.Errorf("block %d = %+v, want %+v", i, g, w)
				}
				if !jsonEqual(g.Input, w.Input) {
					t.Errorf("block %d input = %s, want %s", i, g.Input, w.Input)
				}
			}
		})
	}
}

// jsonEqual reports whether two json.RawMessage values are semantically
// equal, treating empty/nil as equal to each other.
func jsonEqual(a, b json.RawMessage) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	var av, bv any
	_ = json.Unmarshal(a, &av)
	_ = json.Unmarshal(b, &bv)
	aj, _ := json.Marshal(av)
	bj, _ := json.Marshal(bv)
	return string(aj) == string(bj)
}
