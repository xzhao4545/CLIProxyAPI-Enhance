package usage

import (
	"context"
	"time"
)

const (
	StatusSuccess = "success"
	StatusFailure = "failure"
)

type Event struct {
	ID               int64     `json:"id"`
	RequestID        string    `json:"request_id,omitempty"`
	StartedAt        time.Time `json:"started_at"`
	CompletedAt      time.Time `json:"completed_at"`
	DurationMS       int64     `json:"duration_ms"`
	ProviderKey      string    `json:"provider_key"`
	ProviderLabel    string    `json:"provider_label"`
	AuthID           string    `json:"auth_id,omitempty"`
	AuthLabel        string    `json:"auth_label,omitempty"`
	AuthIndex        string    `json:"auth_index,omitempty"`
	AuthPosition     string    `json:"auth_position,omitempty"`
	AuthType         string    `json:"auth_type,omitempty"`
	AuthCategory     string    `json:"auth_category,omitempty"`
	Model            string    `json:"model"`
	ClientModel      string    `json:"client_model,omitempty"`
	ResponseModel    string    `json:"response_model,omitempty"`
	Route            string    `json:"route,omitempty"`
	Stream           bool      `json:"stream"`
	Status           string    `json:"status"`
	HTTPStatus       int       `json:"http_status,omitempty"`
	UpstreamStatus   int       `json:"upstream_status,omitempty"`
	PromptTokens     int64     `json:"prompt_tokens"`
	CompletionTokens int64     `json:"completion_tokens"`
	TotalTokens      int64     `json:"total_tokens"`
	ReasoningTokens  int64     `json:"reasoning_tokens"`
	ReasoningEffort  string    `json:"reasoning_effort,omitempty"`
	CachedTokens     int64     `json:"cached_tokens"`
	TTFTMS           int64     `json:"ttft_ms,omitempty"`
	ClientKeyHash    string    `json:"client_key_hash,omitempty"`
	ErrorStage       string    `json:"error_stage,omitempty"`
	ErrorCode        string    `json:"error_code,omitempty"`
	ErrorMessage     string    `json:"error_message,omitempty"`
	ProviderErrorRaw string    `json:"provider_error_raw,omitempty"`
	MetadataJSON     string    `json:"metadata_json,omitempty"`
}

type QueryFilter struct {
	Provider        string
	RawProvider     string
	ProviderLabel   string
	Model           string
	ClientModel     string
	ResponseModel   string
	Status          string
	ErrorStage      string
	ErrorCode       string
	AuthID          string
	AuthLabel       string
	AuthType        string
	AuthCategory    string
	Stream          string
	ReasoningEffort string
	ClientKeyHash   string
	DateFrom        *time.Time
	DateTo          *time.Time
	Limit           int
	Offset          int
	Sort            string
	Order           string
	IncludeErrorRaw bool
}

type EventsPage struct {
	Events []Event `json:"events"`
	Limit  int     `json:"limit"`
	Offset int     `json:"offset"`
	Total  int64   `json:"total"`
}

type SummaryFilter struct {
	QueryFilter
	GroupBy string
}

type SummaryRow struct {
	Day              string  `json:"day,omitempty"`
	ProviderKey      string  `json:"provider_key,omitempty"`
	ProviderLabel    string  `json:"provider_label,omitempty"`
	Model            string  `json:"model,omitempty"`
	Status           string  `json:"status,omitempty"`
	Requests         int64   `json:"requests"`
	Successful       int64   `json:"successful_requests"`
	Failed           int64   `json:"failed_requests"`
	SuccessRate      float64 `json:"success_rate"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	ReasoningTokens  int64   `json:"reasoning_tokens"`
	CachedTokens     int64   `json:"cached_tokens"`
	TotalTokens      int64   `json:"total_tokens"`
}

type FailureRow struct {
	ErrorStage    string `json:"error_stage,omitempty"`
	ErrorCode     string `json:"error_code,omitempty"`
	ProviderKey   string `json:"provider_key,omitempty"`
	ProviderLabel string `json:"provider_label,omitempty"`
	Model         string `json:"model,omitempty"`
	Requests      int64  `json:"requests"`
	LastMessage   string `json:"last_message,omitempty"`
	LastSeenAt    string `json:"last_seen_at,omitempty"`
}

type FilterOptions struct {
	Providers        []FilterOption `json:"providers"`
	ProviderLabels   []string       `json:"provider_labels"`
	Models           []string       `json:"models"`
	ClientModels     []string       `json:"client_models"`
	ResponseModels   []string       `json:"response_models"`
	AuthLabels       []string       `json:"auth_labels"`
	AuthTypes        []string       `json:"auth_types"`
	AuthCategories   []string       `json:"auth_categories"`
	ReasoningEfforts []string       `json:"reasoning_efforts"`
	Statuses         []string       `json:"statuses"`
	ErrorStages      []string       `json:"error_stages"`
	ErrorCodes       []string       `json:"error_codes"`
}

type FilterOption struct {
	Key          string `json:"key"`
	Label        string `json:"label"`
	AuthID       string `json:"auth_id,omitempty"`
	AuthPosition string `json:"auth_position,omitempty"`
}

type Metrics struct {
	WindowFrom            time.Time        `json:"window_from"`
	WindowTo              time.Time        `json:"window_to"`
	WindowMinutes         float64          `json:"window_minutes"`
	TotalRequests         int64            `json:"total_requests"`
	SuccessfulRequests    int64            `json:"successful_requests"`
	FailedRequests        int64            `json:"failed_requests"`
	SuccessRate           float64          `json:"success_rate"`
	TotalPromptTokens     int64            `json:"total_prompt_tokens"`
	TotalCompletionTokens int64            `json:"total_completion_tokens"`
	TotalReasoningTokens  int64            `json:"total_reasoning_tokens"`
	TotalCachedTokens     int64            `json:"total_cached_tokens"`
	TotalTokens           int64            `json:"total_tokens"`
	RPM                   float64          `json:"rpm"`
	TPM                   float64          `json:"tpm"`
	ProviderSuccessRates  []ProviderMetric `json:"provider_success_rates"`
	ProviderRequestTotals []ProviderMetric `json:"provider_request_totals"`
	ProviderTokenTotals   []ProviderMetric `json:"provider_token_totals"`
	ModelRequestTotals    []ModelMetric    `json:"model_request_totals"`
	ModelTokenTotals      []ModelMetric    `json:"model_token_totals"`
}

type ProviderMetric struct {
	ProviderKey   string  `json:"provider_key"`
	ProviderLabel string  `json:"provider_label"`
	AuthID        string  `json:"auth_id,omitempty"`
	AuthPosition  string  `json:"auth_position,omitempty"`
	Requests      int64   `json:"requests"`
	Successful    int64   `json:"successful_requests"`
	Failed        int64   `json:"failed_requests"`
	Tokens        int64   `json:"tokens"`
	SuccessRate   float64 `json:"success_rate"`
}

type ModelMetric struct {
	Model    string `json:"model"`
	Requests int64  `json:"requests,omitempty"`
	Tokens   int64  `json:"tokens,omitempty"`
}

type QueryService interface {
	QueryEvents(filter QueryFilter) (EventsPage, error)
	QuerySummary(filter SummaryFilter) ([]SummaryRow, error)
	QueryFailures(filter QueryFilter) ([]FailureRow, error)
	QueryFilters(filter QueryFilter) (FilterOptions, error)
	QueryMetrics(filter QueryFilter) (Metrics, error)
}

type ContextQueryService interface {
	QueryEventsContext(ctx context.Context, filter QueryFilter) (EventsPage, error)
	QuerySummaryContext(ctx context.Context, filter SummaryFilter) ([]SummaryRow, error)
	QueryFailuresContext(ctx context.Context, filter QueryFilter) ([]FailureRow, error)
	QueryFiltersContext(ctx context.Context, filter QueryFilter) (FilterOptions, error)
	QueryMetricsContext(ctx context.Context, filter QueryFilter) (Metrics, error)
}
