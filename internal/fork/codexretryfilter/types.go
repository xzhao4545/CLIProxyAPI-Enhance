package codexretryfilter

import (
	"context"
	"time"
)

type AttemptRecord struct {
	RequestID           string
	OccurredAt          time.Time
	ProviderKey         string
	AuthID              string
	AuthLabel           string
	Model               string
	ClientModel         string
	ResponseModel       string
	Stream              bool
	Eligible            bool
	Matched             bool
	ReasoningTokens     *int64
	MatchedLength       *int64
	Action              string
	GuardRetryRemaining int
	Attempt             int
	FinalSuccess        bool
	MetadataJSON        string
}

type HitRecord struct {
	ID                  int64     `json:"id"`
	RequestID           string    `json:"request_id,omitempty"`
	OccurredAt          time.Time `json:"occurred_at"`
	ProviderKey         string    `json:"provider_key"`
	AuthID              string    `json:"auth_id,omitempty"`
	AuthLabel           string    `json:"auth_label,omitempty"`
	Model               string    `json:"model"`
	ClientModel         string    `json:"client_model,omitempty"`
	ResponseModel       string    `json:"response_model,omitempty"`
	Stream              bool      `json:"stream"`
	ReasoningTokens     int64     `json:"reasoning_tokens"`
	MatchedLength       int64     `json:"matched_length"`
	Action              string    `json:"action"`
	GuardRetryRemaining int       `json:"guard_retry_remaining"`
	Attempt             int       `json:"attempt"`
	Retried             bool      `json:"retried"`
	FinalSuccess        bool      `json:"final_success"`
	MetadataJSON        string    `json:"metadata_json,omitempty"`
}

type QueryFilter struct {
	DateFrom      *time.Time
	DateTo        *time.Time
	Model         string
	AuthID        string
	MatchedLength int64
	Action        string
	BeforeTime    *time.Time
	BeforeID      int64
	Limit         int
	Offset        int
}

type HitsResult struct {
	Hits               []HitRecord `json:"hits"`
	NextBeforeOccurred *time.Time  `json:"next_before_occurred_at,omitempty"`
	NextBeforeID       *int64      `json:"next_before_id,omitempty"`
	HasMore            bool        `json:"has_more"`
}

type PruneResult struct {
	Before          time.Time `json:"before"`
	DeletedAttempts int64     `json:"deleted_attempts"`
	DeletedHits     int64     `json:"deleted_hits"`
}

type Breakdown struct {
	Key              string  `json:"key"`
	Label            string  `json:"label,omitempty"`
	Attempts         int64   `json:"attempts"`
	Hits             int64   `json:"hits"`
	HitRate          float64 `json:"hit_rate"`
	RetrySuccessRate float64 `json:"retry_success_rate"`
}

type ReasoningBreakdown struct {
	MatchedLength int64 `json:"matched_length"`
	Hits          int64 `json:"hits"`
}

type ActionBreakdown struct {
	Action string `json:"action"`
	Hits   int64  `json:"hits"`
}

type Stats struct {
	Attempts               int64                `json:"attempts"`
	Hits                   int64                `json:"hits"`
	HitRate                float64              `json:"hit_rate"`
	FinalSuccessesAfterHit int64                `json:"final_successes_after_hit"`
	RetrySuccessRate       float64              `json:"retry_success_rate"`
	InternalRetries        int64                `json:"internal_retries"`
	ConductorRetries       int64                `json:"conductor_retries"`
	ObserveOnlyHits        int64                `json:"observe_only_hits"`
	ByModel                []Breakdown          `json:"by_model"`
	ByAuth                 []Breakdown          `json:"by_auth"`
	ByReasoningTokens      []ReasoningBreakdown `json:"by_reasoning_tokens"`
	ByAction               []ActionBreakdown    `json:"by_action"`
}

type QueryService interface {
	QueryStats(ctx context.Context, filter QueryFilter) (Stats, error)
	QueryHits(ctx context.Context, filter QueryFilter) (HitsResult, error)
	PruneOlderThan(ctx context.Context, before time.Time) (PruneResult, error)
}
