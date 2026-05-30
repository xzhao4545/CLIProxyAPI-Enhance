package usage

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// Record contains the usage statistics captured for a single provider request.
type Record struct {
	Provider      string
	ProviderLabel string
	Model         string
	Alias         string
	APIKey        string
	AuthID        string
	AuthLabel     string
	AuthIndex     string
	AuthType      string
	Source        string
	// ReasoningEffort stores the translated upstream thinking level for request event logs.
	ReasoningEffort string
	RequestedAt     time.Time
	Latency         time.Duration
	TTFT            time.Duration
	Failed          bool
	Fail            Failure
	Detail          Detail
	// ResponseHeaders stores a snapshot of upstream response headers for usage sinks.
	ResponseHeaders http.Header
}

// Failure holds HTTP failure metadata for an upstream request attempt.
type Failure struct {
	StatusCode int
	Stage      string
	Code       string
	Body       string
}

// Detail holds the token usage breakdown.
type Detail struct {
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CachedTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	TotalTokens         int64
}

type requestedModelAliasContextKey struct{}
type reasoningEffortContextKey struct{}
type failureOverrideContextKey struct{}

type failureOverrideState struct {
	mu       sync.RWMutex
	failed   bool
	failure  Failure
	fallback *Record
	records  []failureOverrideRecord
}

type failureOverrideRecord struct {
	manager *Manager
	record  Record
}

// WithRequestedModelAlias stores the client-requested model name for usage sinks.
func WithRequestedModelAlias(ctx context.Context, alias string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return ctx
	}
	return context.WithValue(ctx, requestedModelAliasContextKey{}, alias)
}

// RequestedModelAliasFromContext returns the client-requested model name stored in ctx.
func RequestedModelAliasFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	raw := ctx.Value(requestedModelAliasContextKey{})
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case []byte:
		return strings.TrimSpace(string(value))
	default:
		return ""
	}
}

// WithReasoningEffort stores the client-requested reasoning effort for usage sinks.
func WithReasoningEffort(ctx context.Context, effort string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	effort = strings.TrimSpace(effort)
	if effort == "" {
		return ctx
	}
	return context.WithValue(ctx, reasoningEffortContextKey{}, effort)
}

// ReasoningEffortFromContext returns the client-requested reasoning effort stored in ctx.
func ReasoningEffortFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	raw := ctx.Value(reasoningEffortContextKey{})
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case []byte:
		return strings.TrimSpace(string(value))
	default:
		return ""
	}
}

// WithFailureOverride installs a mutable request-scoped usage failure marker.
// The marker lets late stream wrappers classify an already-started provider
// attempt as failed without coupling every executor to wrapper-level checks.
func WithFailureOverride(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if failureOverrideFromContext(ctx) != nil {
		return ctx
	}
	return context.WithValue(ctx, failureOverrideContextKey{}, &failureOverrideState{})
}

// MarkFailureOverride marks the request-scoped usage record as failed.
func MarkFailureOverride(ctx context.Context, failure Failure) {
	state := failureOverrideFromContext(ctx)
	if state == nil {
		return
	}
	state.mu.Lock()
	state.failed = true
	state.failure = failure
	state.mu.Unlock()
}

// SetFailureOverrideFallback stores a request-scoped fallback usage record.
// It is published only when a failure override is marked and no upstream usage
// record was captured before the stream ended.
func SetFailureOverrideFallback(ctx context.Context, record Record) {
	state := failureOverrideFromContext(ctx)
	if state == nil {
		return
	}
	state.mu.Lock()
	if state.fallback == nil {
		state.fallback = &record
	}
	state.mu.Unlock()
}

// FlushFailureOverrideRecords publishes request-scoped usage records after the
// final stream outcome is known, so late keyword-filter failures can reclassify
// early token-usage records before plugins persist them.
func FlushFailureOverrideRecords(ctx context.Context) {
	state := failureOverrideFromContext(ctx)
	if state == nil {
		return
	}
	state.mu.Lock()
	records := append([]failureOverrideRecord(nil), state.records...)
	if len(records) == 0 && state.failed && state.fallback != nil {
		records = append(records, failureOverrideRecord{manager: DefaultManager(), record: *state.fallback})
	}
	state.records = nil
	state.fallback = nil
	state.mu.Unlock()
	for _, item := range records {
		if item.manager == nil {
			continue
		}
		item.manager.publishNow(ctx, item.record)
	}
}

// ApplyFailureOverride returns a record adjusted by any request-scoped failure marker.
func ApplyFailureOverride(ctx context.Context, record Record) Record {
	state := failureOverrideFromContext(ctx)
	if state == nil {
		return record
	}
	state.mu.RLock()
	failed := state.failed
	failure := state.failure
	state.mu.RUnlock()
	if !failed {
		return record
	}
	record.Failed = true
	record.Fail = mergeFailureOverride(record.Fail, failure)
	return record
}

func failureOverrideFromContext(ctx context.Context) *failureOverrideState {
	if ctx == nil {
		return nil
	}
	state, _ := ctx.Value(failureOverrideContextKey{}).(*failureOverrideState)
	return state
}

func mergeFailureOverride(current, override Failure) Failure {
	if override.StatusCode > 0 {
		current.StatusCode = override.StatusCode
	}
	if strings.TrimSpace(override.Stage) != "" {
		current.Stage = override.Stage
	}
	if strings.TrimSpace(override.Code) != "" {
		current.Code = override.Code
	}
	if strings.TrimSpace(override.Body) != "" {
		current.Body = override.Body
	}
	return current
}

// Plugin consumes usage records emitted by the proxy runtime.
type Plugin interface {
	HandleUsage(ctx context.Context, record Record)
}

type queueItem struct {
	ctx    context.Context
	record Record
}

// Manager maintains a queue of usage records and delivers them to registered plugins.
type Manager struct {
	once     sync.Once
	stopOnce sync.Once
	cancel   context.CancelFunc

	mu       sync.Mutex
	cond     *sync.Cond
	queue    []queueItem
	capacity int
	closed   bool

	pluginsMu sync.RWMutex
	plugins   []Plugin
}

// NewManager constructs a manager with a buffered queue.
func NewManager(buffer int) *Manager {
	if buffer <= 0 {
		buffer = 1
	}
	m := &Manager{capacity: buffer}
	m.cond = sync.NewCond(&m.mu)
	return m
}

// Start launches the background dispatcher. Calling Start multiple times is safe.
func (m *Manager) Start(ctx context.Context) {
	if m == nil {
		return
	}
	m.once.Do(func() {
		if ctx == nil {
			ctx = context.Background()
		}
		var workerCtx context.Context
		workerCtx, m.cancel = context.WithCancel(ctx)
		go m.run(workerCtx)
	})
}

// Stop stops the dispatcher and drains the queue.
func (m *Manager) Stop() {
	if m == nil {
		return
	}
	m.stopOnce.Do(func() {
		if m.cancel != nil {
			m.cancel()
		}
		m.mu.Lock()
		m.closed = true
		m.mu.Unlock()
		m.cond.Broadcast()
	})
}

// Register appends a plugin to the delivery list.
func (m *Manager) Register(plugin Plugin) {
	if m == nil || plugin == nil {
		return
	}
	m.pluginsMu.Lock()
	m.plugins = append(m.plugins, plugin)
	m.pluginsMu.Unlock()
}

// Publish enqueues a usage record for processing. If no plugin is registered
// the record will be discarded downstream.
func (m *Manager) Publish(ctx context.Context, record Record) {
	if m == nil {
		return
	}
	if state := failureOverrideFromContext(ctx); state != nil {
		state.mu.Lock()
		state.records = append(state.records, failureOverrideRecord{manager: m, record: record})
		state.mu.Unlock()
		return
	}
	m.publishNow(ctx, record)
}

func (m *Manager) publishNow(ctx context.Context, record Record) {
	record = ApplyFailureOverride(ctx, record)
	// ensure worker is running even if Start was not called explicitly
	m.Start(context.Background())
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	if len(m.queue) >= m.capacity {
		m.mu.Unlock()
		log.Debug("usage: manager queue full; dropping usage record")
		return
	}
	m.queue = append(m.queue, queueItem{ctx: ctx, record: record})
	m.mu.Unlock()
	m.cond.Signal()
}

func (m *Manager) run(ctx context.Context) {
	for {
		m.mu.Lock()
		for !m.closed && len(m.queue) == 0 {
			m.cond.Wait()
		}
		if len(m.queue) == 0 && m.closed {
			m.mu.Unlock()
			return
		}
		item := m.queue[0]
		m.queue = m.queue[1:]
		m.mu.Unlock()
		m.dispatch(item)
	}
}

func (m *Manager) dispatch(item queueItem) {
	item.record = ApplyFailureOverride(item.ctx, item.record)
	m.pluginsMu.RLock()
	plugins := make([]Plugin, len(m.plugins))
	copy(plugins, m.plugins)
	m.pluginsMu.RUnlock()
	if len(plugins) == 0 {
		return
	}
	for _, plugin := range plugins {
		if plugin == nil {
			continue
		}
		safeInvoke(plugin, item.ctx, item.record)
	}
}

func safeInvoke(plugin Plugin, ctx context.Context, record Record) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("usage: plugin panic recovered: %v", r)
		}
	}()
	plugin.HandleUsage(ctx, record)
}

var defaultManager = NewManager(512)

// DefaultManager returns the global usage manager instance.
func DefaultManager() *Manager { return defaultManager }

// RegisterPlugin registers a plugin on the default manager.
func RegisterPlugin(plugin Plugin) { DefaultManager().Register(plugin) }

// PublishRecord publishes a record using the default manager.
func PublishRecord(ctx context.Context, record Record) { DefaultManager().Publish(ctx, record) }

// StartDefault starts the default manager's dispatcher.
func StartDefault(ctx context.Context) { DefaultManager().Start(ctx) }

// StopDefault stops the default manager's dispatcher.
func StopDefault() { DefaultManager().Stop() }
