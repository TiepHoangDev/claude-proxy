package stats

import (
	"sync"
	"time"
)

type RequestLog struct {
	ID                       int64
	Timestamp                time.Time
	Method                   string
	Path                     string
	Model                    string
	Status                   int
	InputTokens              int
	OutputTokens             int
	CacheCreationInputTokens int
	CacheReadInputTokens     int
	TotalInputTokens         int
	DurationMs               int64
	Streaming                bool
	ToolUses                 []ToolUse
	ToolResults              []ToolResult
	ResponseBlocks           []TimelineBlock
	RequestBody              string
	ResponseBody             string
}

type Totals struct {
	RequestCount             int
	InputTokens              int
	OutputTokens             int
	CacheCreationInputTokens int
	CacheReadInputTokens     int
	TotalInputTokens         int
}

type Store struct {
	mu     sync.Mutex
	items  []RequestLog
	nextID int64
	cap    int
}

func NewStore(capacity int) *Store {
	return &Store{cap: capacity}
}

func (s *Store) Add(item RequestLog) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	item.ID = s.nextID
	s.items = append(s.items, item)
	if len(s.items) > s.cap {
		s.items = s.items[len(s.items)-s.cap:]
	}
}

// Recent returns logged requests, newest first.
func (s *Store) Recent() []RequestLog {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]RequestLog, len(s.items))
	for i, item := range s.items {
		out[len(s.items)-1-i] = item
	}
	return out
}

// Get returns the request log entry with the given ID, if present.
func (s *Store) Get(id int64) (RequestLog, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.items {
		if item.ID == id {
			return item, true
		}
	}
	return RequestLog{}, false
}

func (s *Store) Totals() Totals {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := Totals{RequestCount: len(s.items)}
	for _, item := range s.items {
		t.InputTokens += item.InputTokens
		t.OutputTokens += item.OutputTokens
		t.CacheCreationInputTokens += item.CacheCreationInputTokens
		t.CacheReadInputTokens += item.CacheReadInputTokens
		t.TotalInputTokens += item.TotalInputTokens
	}
	return t
}
