package usage

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type PositionFunc func(authID string) string

type Handler struct {
	service      QueryService
	positionFunc PositionFunc
}

func NewHandler(service QueryService) *Handler {
	return &Handler{service: service}
}

func (h *Handler) SetPositionFunc(fn PositionFunc) {
	if h == nil {
		return
	}
	h.positionFunc = fn
}

func (h *Handler) Register(group gin.IRoutes) {
	h.RegisterAt(group, "/usage")
	h.RegisterAt(group, "/api/usage")
}

func (h *Handler) RegisterAt(group gin.IRoutes, base string) {
	if h == nil || group == nil {
		return
	}
	base = "/" + strings.Trim(strings.TrimSpace(base), "/")
	group.GET(base+"/events", h.GetEvents)
	group.GET(base+"/summary", h.GetSummary)
	group.GET(base+"/failures", h.GetFailures)
	group.GET(base+"/filters", h.GetFilters)
	group.GET(base+"/metrics", h.GetMetrics)
}

func (h *Handler) GetEvents(c *gin.Context) {
	filter, ok := parseQueryFilter(c)
	if !ok {
		return
	}
	filter.IncludeErrorRaw = parseBool(c.Query("include_error_raw"))
	page, err := h.queryService(c).QueryEventsContext(c.Request.Context(), filter)
	if err == nil && h.positionFunc != nil {
		for i := range page.Events {
			if page.Events[i].AuthID != "" {
				page.Events[i].AuthPosition = h.positionFunc(page.Events[i].AuthID)
			}
		}
	}
	writeUsageResponse(c, page, err)
}

func (h *Handler) GetSummary(c *gin.Context) {
	filter, ok := parseQueryFilter(c)
	if !ok {
		return
	}
	groupBy := strings.TrimSpace(c.DefaultQuery("group_by", "provider"))
	rows, err := h.queryService(c).QuerySummaryContext(c.Request.Context(), SummaryFilter{QueryFilter: filter, GroupBy: groupBy})
	writeUsageResponse(c, gin.H{"group_by": groupBy, "rows": rows}, err)
}

func (h *Handler) GetFailures(c *gin.Context) {
	filter, ok := parseQueryFilter(c)
	if !ok {
		return
	}
	rows, err := h.queryService(c).QueryFailuresContext(c.Request.Context(), filter)
	writeUsageResponse(c, gin.H{"failures": rows}, err)
}

func (h *Handler) GetFilters(c *gin.Context) {
	filter, ok := parseQueryFilter(c)
	if !ok {
		return
	}
	options, err := h.queryService(c).QueryFiltersContext(c.Request.Context(), filter)
	if err == nil && h.positionFunc != nil {
		for i := range options.Providers {
			if options.Providers[i].AuthID != "" {
				options.Providers[i].AuthPosition = h.positionFunc(options.Providers[i].AuthID)
			}
		}
	}
	writeUsageResponse(c, options, err)
}

func (h *Handler) GetMetrics(c *gin.Context) {
	filter, ok := parseQueryFilter(c)
	if !ok {
		return
	}
	metrics, err := h.queryService(c).QueryMetricsContext(c.Request.Context(), filter)
	if err == nil && h.positionFunc != nil {
		for i := range metrics.ProviderSuccessRates {
			if metrics.ProviderSuccessRates[i].AuthID != "" {
				metrics.ProviderSuccessRates[i].AuthPosition = h.positionFunc(metrics.ProviderSuccessRates[i].AuthID)
			}
		}
		// all three slices reference the same objects, so one pass is enough
	}
	writeUsageResponse(c, metrics, err)
}

func (h *Handler) queryService(c *gin.Context) ContextQueryService {
	if service, ok := h.service.(ContextQueryService); ok {
		return service
	}
	return contextQueryService{service: h.service}
}

type contextQueryService struct {
	service QueryService
}

func (s contextQueryService) QueryEventsContext(_ context.Context, filter QueryFilter) (EventsPage, error) {
	return s.service.QueryEvents(filter)
}

func (s contextQueryService) QuerySummaryContext(_ context.Context, filter SummaryFilter) ([]SummaryRow, error) {
	return s.service.QuerySummary(filter)
}

func (s contextQueryService) QueryFailuresContext(_ context.Context, filter QueryFilter) ([]FailureRow, error) {
	return s.service.QueryFailures(filter)
}

func (s contextQueryService) QueryFiltersContext(_ context.Context, filter QueryFilter) (FilterOptions, error) {
	return s.service.QueryFilters(filter)
}

func (s contextQueryService) QueryMetricsContext(_ context.Context, filter QueryFilter) (Metrics, error) {
	return s.service.QueryMetrics(filter)
}

func parseQueryFilter(c *gin.Context) (QueryFilter, bool) {
	dateFrom, errFrom := parseTimeQuery(c.Query("date_from"))
	if errFrom != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errFrom.Error()})
		return QueryFilter{}, false
	}
	dateTo, errTo := parseTimeQuery(c.Query("date_to"))
	if errTo != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errTo.Error()})
		return QueryFilter{}, false
	}
	limit, ok := parseIntQuery(c, "limit", defaultEventsLimit)
	if !ok {
		return QueryFilter{}, false
	}
	offset, ok := parseIntQuery(c, "offset", 0)
	if !ok {
		return QueryFilter{}, false
	}
	return QueryFilter{
		Provider:        c.Query("provider"),
		RawProvider:     c.Query("raw_provider"),
		ProviderLabel:   c.Query("provider_label"),
		Model:           c.Query("model"),
		ClientModel:     c.Query("client_model"),
		ResponseModel:   c.Query("response_model"),
		Status:          c.Query("status"),
		ErrorStage:      c.Query("error_stage"),
		ErrorCode:       c.Query("error_code"),
		AuthID:          c.Query("auth_id"),
		AuthLabel:       c.Query("auth_label"),
		AuthType:        c.Query("auth_type"),
		AuthCategory:    c.Query("auth_category"),
		Stream:          c.Query("stream"),
		ReasoningEffort: c.Query("reasoning_effort"),
		ClientKeyHash:   c.Query("client_key_hash"),
		DateFrom:        dateFrom,
		DateTo:          dateTo,
		Limit:           limit,
		Offset:          offset,
		Sort:            c.Query("sort"),
		Order:           c.Query("order"),
	}, true
}

func parseIntQuery(c *gin.Context, name string, fallback int) (int, bool) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return fallback, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": name + " must be an integer"})
		return 0, false
	}
	return value, true
}

func parseBool(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	parsed, err := strconv.ParseBool(value)
	return err == nil && parsed
}

func writeUsageResponse(c *gin.Context, payload any, err error) {
	if err == nil {
		c.JSON(http.StatusOK, payload)
		return
	}
	if errors.Is(err, ErrDisabled) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "usage statistics are disabled"})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
}
