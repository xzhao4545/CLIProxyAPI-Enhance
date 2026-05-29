package usage

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	internallogging "github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
	log "github.com/sirupsen/logrus"
)

type Recorder struct {
	mu           sync.RWMutex
	cfg          Config
	store        *SQLiteStore
	pendingClose []*SQLiteStore
	queue        chan Event
	stop         chan struct{}
	stopOnce     sync.Once
	done         chan struct{}
	closed       bool
}

func NewRecorderFromConfig(ctx context.Context, cfg Config, configFilePath string) (*Recorder, error) {
	cfg = normalizeConfig(cfg)
	var store *SQLiteStore
	if cfg.Enabled {
		opened, err := OpenSQLiteStore(ctx, ResolveSQLitePath(cfg.SQLitePath, configFilePath))
		if err != nil {
			return nil, err
		}
		store = opened
	}
	return NewRecorder(cfg, store), nil
}

func NewRecorder(cfg Config, store *SQLiteStore) *Recorder {
	cfg = normalizeConfig(cfg)
	r := &Recorder{
		cfg:   cfg,
		store: store,
		queue: make(chan Event, 1024),
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}
	go r.run()
	return r
}

func (r *Recorder) UpdateConfig(ctx context.Context, cfg Config, configFilePath string) error {
	if r == nil {
		return nil
	}
	cfg = normalizeConfig(cfg)
	if ctx == nil {
		ctx = context.Background()
	}

	var nextStore *SQLiteStore
	var err error
	if cfg.Enabled {
		nextStore, err = OpenSQLiteStore(ctx, ResolveSQLitePath(cfg.SQLitePath, configFilePath))
		if err != nil {
			return err
		}
	}

	r.mu.Lock()
	oldStore := r.store
	r.cfg = cfg
	r.store = nextStore
	if oldStore != nil {
		r.pendingClose = append(r.pendingClose, oldStore)
	}
	r.mu.Unlock()

	return nil
}

func (r *Recorder) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
	r.stopOnce.Do(func() {
		close(r.stop)
		<-r.done
	})
	r.mu.Lock()
	store := r.store
	r.store = nil
	pendingClose := r.pendingClose
	r.pendingClose = nil
	r.mu.Unlock()
	for _, oldStore := range pendingClose {
		if oldStore != nil {
			_ = oldStore.Close()
		}
	}
	if store != nil {
		return store.Close()
	}
	return nil
}

func (r *Recorder) QueryEvents(filter QueryFilter) (EventsPage, error) {
	return r.QueryEventsContext(context.Background(), filter)
}

func (r *Recorder) QueryEventsContext(ctx context.Context, filter QueryFilter) (EventsPage, error) {
	store := r.currentStore()
	if store == nil {
		return EventsPage{}, ErrDisabled
	}
	return store.QueryEventsContext(ctx, filter)
}

func (r *Recorder) QuerySummary(filter SummaryFilter) ([]SummaryRow, error) {
	return r.QuerySummaryContext(context.Background(), filter)
}

func (r *Recorder) QuerySummaryContext(ctx context.Context, filter SummaryFilter) ([]SummaryRow, error) {
	store := r.currentStore()
	if store == nil {
		return nil, ErrDisabled
	}
	return store.QuerySummaryContext(ctx, filter)
}

func (r *Recorder) QueryFailures(filter QueryFilter) ([]FailureRow, error) {
	return r.QueryFailuresContext(context.Background(), filter)
}

func (r *Recorder) QueryFailuresContext(ctx context.Context, filter QueryFilter) ([]FailureRow, error) {
	store := r.currentStore()
	if store == nil {
		return nil, ErrDisabled
	}
	return store.QueryFailuresContext(ctx, filter)
}

func (r *Recorder) QueryFilters(filter QueryFilter) (FilterOptions, error) {
	return r.QueryFiltersContext(context.Background(), filter)
}

func (r *Recorder) QueryFiltersContext(ctx context.Context, filter QueryFilter) (FilterOptions, error) {
	store := r.currentStore()
	if store == nil {
		return FilterOptions{}, ErrDisabled
	}
	return store.QueryFiltersContext(ctx, filter)
}

func (r *Recorder) QueryMetrics(filter QueryFilter) (Metrics, error) {
	return r.QueryMetricsContext(context.Background(), filter)
}

func (r *Recorder) QueryMetricsContext(ctx context.Context, filter QueryFilter) (Metrics, error) {
	store := r.currentStore()
	if store == nil {
		return Metrics{}, ErrDisabled
	}
	return store.QueryMetricsContext(ctx, filter)
}

func (r *Recorder) HandleUsage(ctx context.Context, record coreusage.Record) {
	if r == nil {
		return
	}
	r.mu.RLock()
	if r.closed || !r.cfg.Enabled || r.store == nil || r.queue == nil {
		r.mu.RUnlock()
		return
	}
	cfg := r.cfg
	queue := r.queue
	r.mu.RUnlock()

	event := buildEvent(ctx, record, cfg)
	if event.ProviderKey == "" {
		event.ProviderKey = "unknown"
	}
	if event.ProviderLabel == "" {
		event.ProviderLabel = event.ProviderKey
	}
	if event.Model == "" {
		event.Model = "unknown"
	}
	select {
	case queue <- event:
	default:
		log.Debug("usage sqlite queue full; dropping usage event")
	}
}

func (r *Recorder) currentStore() *SQLiteStore {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed || !r.cfg.Enabled {
		return nil
	}
	return r.store
}

func (r *Recorder) insertStore() *SQLiteStore {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if !r.cfg.Enabled {
		return nil
	}
	return r.store
}

func (r *Recorder) run() {
	defer close(r.done)
	for {
		select {
		case <-r.stop:
			r.drain()
			return
		case event := <-r.queue:
			r.insertBestEffort(event)
		}
	}
}

func (r *Recorder) drain() {
	for {
		select {
		case event := <-r.queue:
			r.insertBestEffort(event)
		default:
			return
		}
	}
}

func (r *Recorder) insertBestEffort(event Event) {
	store := r.insertStore()
	if store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := store.InsertEvent(ctx, event); err != nil {
		log.WithError(err).Debug("failed to persist usage event")
	}
	r.closePendingStores()
}

func (r *Recorder) closePendingStores() {
	if r == nil {
		return
	}
	r.mu.Lock()
	pendingClose := r.pendingClose
	r.pendingClose = nil
	r.mu.Unlock()
	for _, store := range pendingClose {
		if store != nil {
			_ = store.Close()
		}
	}
}

func buildEvent(ctx context.Context, record coreusage.Record, cfg Config) Event {
	startedAt := record.RequestedAt
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	startedAt = startedAt.UTC()
	duration := record.Latency
	if duration < 0 {
		duration = 0
	}
	completedAt := startedAt.Add(duration).UTC()
	providerKey := strings.TrimSpace(record.Provider)
	providerLabel := strings.TrimSpace(record.ProviderLabel)
	authLabel := strings.TrimSpace(record.AuthLabel)
	if providerLabel == "" {
		providerLabel = authLabel
	}
	if providerLabel == "" {
		providerLabel = providerKey
	}
	if override := cfg.ProviderLabels[strings.ToLower(providerKey)]; override != "" {
		providerLabel = override
	}

	status := StatusSuccess
	failed := record.Failed
	if !failed {
		failed = !resolveSuccess(ctx)
	}
	if failed {
		status = StatusFailure
	}
	httpStatus := internallogging.GetResponseStatus(ctx)
	failStatus := record.Fail.StatusCode
	if failStatus <= 0 && failed {
		failStatus = httpStatus
	}
	if httpStatus <= 0 && failed {
		httpStatus = failStatus
	}
	if httpStatus <= 0 && !failed {
		httpStatus = http.StatusOK
	}
	message, raw := sanitizeProviderError(record.Fail.Body, cfg.MaxProviderErrorBytes)
	if message == "" && failed {
		message = http.StatusText(httpStatus)
	}

	errorStage := strings.TrimSpace(record.Fail.Stage)
	if failed && errorStage == "" {
		errorStage = inferErrorStage(record.Fail, httpStatus)
	}
	errorCode := strings.TrimSpace(record.Fail.Code)
	if failed && errorCode == "" && httpStatus > 0 {
		errorCode = http.StatusText(httpStatus)
	}

	total := record.Detail.TotalTokens
	if total == 0 {
		total = record.Detail.InputTokens + record.Detail.OutputTokens + record.Detail.ReasoningTokens
	}
	metadataJSON := metadataFromRecord(record)
	return Event{
		RequestID:        strings.TrimSpace(internallogging.GetRequestID(ctx)),
		StartedAt:        startedAt,
		CompletedAt:      completedAt,
		DurationMS:       duration.Milliseconds(),
		ProviderKey:      providerKey,
		ProviderLabel:    providerLabel,
		AuthID:           strings.TrimSpace(record.AuthID),
		AuthLabel:        authLabel,
		AuthIndex:        strings.TrimSpace(record.AuthIndex),
		Model:            strings.TrimSpace(record.Model),
		ClientModel:      strings.TrimSpace(record.Alias),
		Route:            strings.TrimSpace(internallogging.GetEndpoint(ctx)),
		Status:           status,
		HTTPStatus:       httpStatus,
		UpstreamStatus:   failStatus,
		PromptTokens:     record.Detail.InputTokens,
		CompletionTokens: record.Detail.OutputTokens,
		TotalTokens:      total,
		ReasoningTokens:  record.Detail.ReasoningTokens,
		CachedTokens:     record.Detail.CachedTokens,
		ClientKeyHash:    hashClientKey(record.APIKey),
		ErrorStage:       errorStage,
		ErrorCode:        errorCode,
		ErrorMessage:     message,
		ProviderErrorRaw: raw,
		MetadataJSON:     metadataJSON,
	}
}

func resolveSuccess(ctx context.Context) bool {
	status := internallogging.GetResponseStatus(ctx)
	return status == 0 || status < http.StatusBadRequest
}

func inferErrorStage(failure coreusage.Failure, status int) string {
	if strings.TrimSpace(failure.Stage) != "" {
		return strings.TrimSpace(failure.Stage)
	}
	if failure.StatusCode > 0 || status >= http.StatusBadRequest {
		return "upstream_response"
	}
	if strings.TrimSpace(failure.Body) != "" {
		return "upstream_request"
	}
	return "unknown"
}

func metadataFromRecord(record coreusage.Record) string {
	meta := map[string]any{}
	if value := strings.TrimSpace(record.AuthType); value != "" {
		meta["auth_type"] = value
	}
	if value := strings.TrimSpace(record.Source); value != "" {
		meta["source"] = value
	}
	if value := strings.TrimSpace(record.ReasoningEffort); value != "" {
		meta["reasoning_effort"] = value
	}
	if record.TTFT > 0 {
		meta["ttft_ms"] = record.TTFT.Milliseconds()
	}
	if len(meta) == 0 {
		return ""
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return ""
	}
	return string(data)
}
