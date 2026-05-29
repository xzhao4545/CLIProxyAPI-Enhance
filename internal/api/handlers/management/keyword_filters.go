package management

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

// GetKeywordFilters returns all keyword filter rules.
func (h *Handler) GetKeywordFilters(c *gin.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()

	rules := h.cfg.KeywordFilters
	if rules == nil {
		rules = []config.KeywordFilterRule{}
	}
	c.JSON(http.StatusOK, gin.H{"keyword-filters": rules})
}

// PutKeywordFilters replaces all keyword filter rules.
func (h *Handler) PutKeywordFilters(c *gin.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()

	var body struct {
		Rules []config.KeywordFilterRule `json:"keyword-filters"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body, expected {\"keyword-filters\": [...]}"})
		return
	}
	if body.Rules == nil {
		body.Rules = []config.KeywordFilterRule{}
	}
	for i := range body.Rules {
		if body.Rules[i].MatchMode == "" {
			body.Rules[i].MatchMode = "anywhere"
		}
	}
	h.cfg.KeywordFilters = body.Rules
	h.persistLocked(c)
}

// PatchKeywordFilter updates a single keyword filter rule at the given index.
func (h *Handler) PatchKeywordFilter(c *gin.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()

	var body struct {
		Index *int                      `json:"index"`
		Rule  *config.KeywordFilterRule `json:"rule"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Index == nil || body.Rule == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body, requires index and rule"})
		return
	}
	idx := *body.Index
	if idx < 0 || idx >= len(h.cfg.KeywordFilters) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "index out of range"})
		return
	}
	if body.Rule.MatchMode == "" {
		body.Rule.MatchMode = "anywhere"
	}
	h.cfg.KeywordFilters[idx] = *body.Rule
	h.persistLocked(c)
}

// DeleteKeywordFilter removes a keyword filter rule at the given index.
func (h *Handler) DeleteKeywordFilter(c *gin.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()

	idxStr := c.Query("index")
	if idxStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing index query parameter"})
		return
	}
	var idx int
	if _, err := fmt.Sscanf(idxStr, "%d", &idx); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid index"})
		return
	}
	if idx < 0 || idx >= len(h.cfg.KeywordFilters) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "index out of range"})
		return
	}
	h.cfg.KeywordFilters = append(h.cfg.KeywordFilters[:idx], h.cfg.KeywordFilters[idx+1:]...)
	h.persistLocked(c)
}
