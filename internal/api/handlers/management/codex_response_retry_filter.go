package management

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/fork/codexretryfilter"
)

type codexRetryFilterConfigBody struct {
	Enabled               *bool    `json:"enabled"`
	Models                []string `json:"models"`
	ReasoningTokenLengths []int64  `json:"reasoning-token-lengths"`
	InterceptStreaming    *bool    `json:"intercept-streaming"`
	InterceptNonStreaming *bool    `json:"intercept-non-streaming"`
	GuardRetryAttempts    *int     `json:"guard-retry-attempts"`
}

func (h *Handler) GetCodexResponseRetryFilter(c *gin.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()
	c.JSON(http.StatusOK, gin.H{"codex-response-retry-filter": codexretryfilter.Normalize(h.cfg.CodexResponseRetryFilter)})
}

func (h *Handler) PutCodexResponseRetryFilter(c *gin.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()

	var body struct {
		Config codexRetryFilterConfigBody `json:"codex-response-retry-filter"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body, expected {\"codex-response-retry-filter\": {...}}"})
		return
	}
	if err := validateExplicitCodexRetryFilterBody(body.Config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	runtime := codexretryfilter.Normalize(h.cfg.CodexResponseRetryFilter)
	applyCodexRetryFilterBody(&runtime, body.Config)
	if err := codexretryfilter.Validate(runtime); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.cfg.CodexResponseRetryFilter = codexretryfilter.ToConfig(runtime)
	h.persistLocked(c)
}

func (h *Handler) PatchCodexResponseRetryFilter(c *gin.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()

	var body codexRetryFilterConfigBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	if err := validateExplicitCodexRetryFilterBody(body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	runtime := codexretryfilter.Normalize(h.cfg.CodexResponseRetryFilter)
	applyCodexRetryFilterBody(&runtime, body)
	if err := codexretryfilter.Validate(runtime); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.cfg.CodexResponseRetryFilter = codexretryfilter.ToConfig(runtime)
	h.persistLocked(c)
}

func (h *Handler) GetCodexResponseRetryFilterStats(c *gin.Context) {
	service := h.codexRetryFilterQueryService()
	if service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "codex response retry filter stats unavailable"})
		return
	}
	filter, ok := parseCodexRetryFilterQuery(c)
	if !ok {
		return
	}
	stats, err := service.QueryStats(c.Request.Context(), filter)
	writeCodexRetryFilterResponse(c, stats, err)
}

func (h *Handler) GetCodexResponseRetryFilterHits(c *gin.Context) {
	service := h.codexRetryFilterQueryService()
	if service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "codex response retry filter stats unavailable"})
		return
	}
	filter, ok := parseCodexRetryFilterQuery(c)
	if !ok {
		return
	}
	hits, err := service.QueryHits(c.Request.Context(), filter)
	writeCodexRetryFilterResponse(c, gin.H{"hits": hits}, err)
}

func applyCodexRetryFilterBody(runtime *codexretryfilter.RuntimeConfig, body codexRetryFilterConfigBody) {
	if runtime == nil {
		return
	}
	if body.Enabled != nil {
		runtime.Enabled = *body.Enabled
	}
	if body.Models != nil {
		runtime.Models = body.Models
	}
	if body.ReasoningTokenLengths != nil {
		runtime.ReasoningTokenLengths = body.ReasoningTokenLengths
	}
	if body.InterceptStreaming != nil {
		runtime.InterceptStreaming = *body.InterceptStreaming
	}
	if body.InterceptNonStreaming != nil {
		runtime.InterceptNonStreaming = *body.InterceptNonStreaming
	}
	if body.GuardRetryAttempts != nil {
		runtime.GuardRetryAttempts = *body.GuardRetryAttempts
	}
}

func validateExplicitCodexRetryFilterBody(body codexRetryFilterConfigBody) error {
	if body.Models != nil {
		hasModel := false
		for _, model := range body.Models {
			if strings.TrimSpace(model) != "" {
				hasModel = true
				break
			}
		}
		if !hasModel {
			return fmt.Errorf("models must contain at least one non-empty pattern")
		}
	}
	if body.ReasoningTokenLengths != nil {
		if len(body.ReasoningTokenLengths) == 0 {
			return fmt.Errorf("reasoning-token-lengths must contain at least one positive integer")
		}
		for _, length := range body.ReasoningTokenLengths {
			if length <= 0 {
				return fmt.Errorf("reasoning-token-lengths must contain positive integers")
			}
		}
	}
	return nil
}

func parseCodexRetryFilterQuery(c *gin.Context) (codexretryfilter.QueryFilter, bool) {
	dateFrom, ok := parseCodexRetryFilterTime(c, "from")
	if !ok {
		return codexretryfilter.QueryFilter{}, false
	}
	dateTo, ok := parseCodexRetryFilterTime(c, "to")
	if !ok {
		return codexretryfilter.QueryFilter{}, false
	}
	matchedLength, ok := parseCodexRetryFilterInt64(c, "matched_length")
	if !ok {
		return codexretryfilter.QueryFilter{}, false
	}
	limit, ok := parseCodexRetryFilterInt(c, "limit")
	if !ok {
		return codexretryfilter.QueryFilter{}, false
	}
	offset, ok := parseCodexRetryFilterInt(c, "offset")
	if !ok {
		return codexretryfilter.QueryFilter{}, false
	}
	return codexretryfilter.QueryFilter{
		DateFrom:      dateFrom,
		DateTo:        dateTo,
		Model:         c.Query("model"),
		AuthID:        c.Query("auth_id"),
		MatchedLength: matchedLength,
		Action:        c.Query("action"),
		Limit:         limit,
		Offset:        offset,
	}, true
}

func parseCodexRetryFilterTime(c *gin.Context, key string) (*time.Time, bool) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return nil, true
	}
	if unixMS, err := strconv.ParseInt(raw, 10, 64); err == nil {
		t := time.UnixMilli(unixMS).UTC()
		return &t, true
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid " + key + " time"})
		return nil, false
	}
	t = t.UTC()
	return &t, true
}

func parseCodexRetryFilterInt64(c *gin.Context, key string) (int64, bool) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return 0, true
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid " + key})
		return 0, false
	}
	return value, true
}

func parseCodexRetryFilterInt(c *gin.Context, key string) (int, bool) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return 0, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid " + key})
		return 0, false
	}
	return value, true
}

func writeCodexRetryFilterResponse(c *gin.Context, payload any, err error) {
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, payload)
}
