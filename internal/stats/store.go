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

// DeepSeekBalance holds the result of a /user/balance query.
type DeepSeekBalance struct {
	IsAvailable bool   `json:"isAvailable"`
	Currency    string `json:"currency"`
	Total       string `json:"total"`
	Display     string `json:"display"`
}

// ClaudeUsage holds usage/quota info from the OAuth usage API for
// Claude subscription (Pro/Max) users.
type ClaudeUsage struct {
	FiveHourPercent float64 `json:"fiveHourPercent"`
	FiveHourReset   string  `json:"fiveHourReset"`
	SevenDayPercent float64 `json:"sevenDayPercent"`
	SevenDayReset   string  `json:"sevenDayReset"`
}

// HealthState tracks the current API key and rate limit health.
type HealthState struct {
	APIKeyError    string           `json:"apiKeyError,omitempty"`
	RateLimit      RateLimitInfo    `json:"rateLimit"`
	ClaudeUsage    *ClaudeUsage     `json:"claudeUsage,omitempty"`
	DeepSeek       *DeepSeekBalance `json:"deepseek,omitempty"`
	LastCheck      time.Time        `json:"lastCheck"`
}

type Store struct {
	mu          sync.Mutex
	items       []RequestLog
	nextID      int64
	cap         int
	health      HealthState
	lastAuthKey string
}

func (s *Store) SetAuth(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if key != "" {
		s.lastAuthKey = key
	}
}

func (s *Store) GetAuth() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastAuthKey
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

func (s *Store) UpdateClaudeUsage(u *ClaudeUsage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.health.ClaudeUsage = u
}

func (s *Store) UpdateDeepSeekBalance(bal *DeepSeekBalance) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.health.DeepSeek = bal
}

func (s *Store) UpdateHealth(rl RateLimitInfo, apiKeyError string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.health.APIKeyError = apiKeyError
	s.health.RateLimit = rl
	s.health.LastCheck = time.Now()
}

func (s *Store) Health() HealthState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.health
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
