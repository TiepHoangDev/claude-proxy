package stats

import (
	"encoding/json"
	"testing"
)

func TestExtractToolUses(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []ToolUse
	}{
		{
			name: "text and tool_use blocks",
			body: `{
				"content": [
					{"type": "text", "text": "Let me check that file."},
					{"type": "tool_use", "id": "toolu_01", "name": "Read", "input": {"file_path": "/tmp/foo.go"}},
					{"type": "tool_use", "id": "toolu_02", "name": "mcp__github__search_repositories", "input": {"query": "claude-proxy"}}
				]
			}`,
			want: []ToolUse{
				{ID: "toolu_01", Name: "Read", Input: json.RawMessage(`{"file_path": "/tmp/foo.go"}`)},
				{ID: "toolu_02", Name: "mcp__github__search_repositories", Input: json.RawMessage(`{"query": "claude-proxy"}`)},
			},
		},
		{
			name: "no tool_use blocks",
			body: `{"content": [{"type": "text", "text": "hello"}]}`,
			want: nil,
		},
		{
			name: "invalid json",
			body: `not json`,
			want: nil,
		},
		{
			name: "empty body",
			body: ``,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractToolUses([]byte(tt.body))
			if len(got) != len(tt.want) {
				t.Fatalf("got %d tool uses, want %d (%+v)", len(got), len(tt.want), got)
			}
			for i := range tt.want {
				if got[i].ID != tt.want[i].ID || got[i].Name != tt.want[i].Name {
					t.Errorf("tool use %d = %+v, want %+v", i, got[i], tt.want[i])
				}
				var gotInput, wantInput any
				_ = json.Unmarshal(got[i].Input, &gotInput)
				_ = json.Unmarshal(tt.want[i].Input, &wantInput)
				gotJSON, _ := json.Marshal(gotInput)
				wantJSON, _ := json.Marshal(wantInput)
				if string(gotJSON) != string(wantJSON) {
					t.Errorf("tool use %d input = %s, want %s", i, got[i].Input, tt.want[i].Input)
				}
			}
		})
	}
}

func TestExtractToolResults(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []ToolResult
	}{
		{
			name: "tool_result blocks across messages",
			body: `{
				"messages": [
					{"role": "user", "content": "hi"},
					{"role": "assistant", "content": [{"type": "tool_use", "id": "toolu_01", "name": "Read", "input": {}}]},
					{"role": "user", "content": [
						{"type": "tool_result", "tool_use_id": "toolu_01", "content": "file contents here"},
						{"type": "tool_result", "tool_use_id": "toolu_02", "is_error": true, "content": [{"type": "text", "text": "boom"}]}
					]}
				]
			}`,
			want: []ToolResult{
				{ToolUseID: "toolu_01", IsError: false, Size: len(`"file contents here"`)},
				{ToolUseID: "toolu_02", IsError: true, Size: len(`[{"type": "text", "text": "boom"}]`)},
			},
		},
		{
			name: "plain string content, no results",
			body: `{"messages": [{"role": "user", "content": "hello"}]}`,
			want: nil,
		},
		{
			name: "invalid json",
			body: `not json`,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractToolResults([]byte(tt.body))
			if len(got) != len(tt.want) {
				t.Fatalf("got %d tool results, want %d (%+v)", len(got), len(tt.want), got)
			}
			for i := range tt.want {
				if got[i].ToolUseID != tt.want[i].ToolUseID || got[i].IsError != tt.want[i].IsError {
					t.Errorf("tool result %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestToolNames(t *testing.T) {
	uses := []ToolUse{
		{Name: "Read"},
		{Name: "Grep"},
		{Name: "mcp__github__search_repositories"},
	}
	want := []string{"Read", "Grep", "mcp__github__search_repositories"}
	got := ToolNames(uses)
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("name %d = %q, want %q", i, got[i], want[i])
		}
	}

	if ToolNames(nil) != nil {
		t.Errorf("ToolNames(nil) = %v, want nil", ToolNames(nil))
	}
}
