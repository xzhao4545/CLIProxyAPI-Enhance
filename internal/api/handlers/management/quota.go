package management

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

// Quota exceeded toggles
func (h *Handler) GetSwitchProject(c *gin.Context) {
	c.JSON(200, gin.H{"switch-project": h.cfg.QuotaExceeded.SwitchProject})
}
func (h *Handler) PutSwitchProject(c *gin.Context) {
	h.updateBoolField(c, func(v bool) { h.cfg.QuotaExceeded.SwitchProject = v })
}

func (h *Handler) GetSwitchPreviewModel(c *gin.Context) {
	c.JSON(200, gin.H{"switch-preview-model": h.cfg.QuotaExceeded.SwitchPreviewModel})
}
func (h *Handler) PutSwitchPreviewModel(c *gin.Context) {
	h.updateBoolField(c, func(v bool) { h.cfg.QuotaExceeded.SwitchPreviewModel = v })
}

// ListUnavailable returns currently unavailable/cooling provider or model entries.
// Query params:
//   - provider
//   - auth_index
//   - model
//   - active_only (default true)
//   - include_nonblocking (default false)
func (h *Handler) ListUnavailable(c *gin.Context) {
	if h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "core auth manager unavailable"})
		return
	}

	activeOnly := true
	if raw := strings.TrimSpace(c.Query("active_only")); raw != "" {
		parsed, errParse := strconv.ParseBool(raw)
		if errParse != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "active_only must be a boolean"})
			return
		}
		activeOnly = parsed
	}
	includeNonBlocking := false
	if raw := strings.TrimSpace(c.Query("include_nonblocking")); raw != "" {
		parsed, errParse := strconv.ParseBool(raw)
		if errParse != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "include_nonblocking must be a boolean"})
			return
		}
		includeNonBlocking = parsed
	}

	items := h.authManager.ListUnavailable(coreauth.UnavailableFilter{
		Provider:           c.Query("provider"),
		AuthIndex:          c.Query("auth_index"),
		Model:              c.Query("model"),
		ActiveOnly:         activeOnly,
		IncludeNonBlocking: includeNonBlocking,
	})
	if items == nil {
		items = []coreauth.UnavailableEntry{}
	}
	c.JSON(http.StatusOK, gin.H{
		"items": items,
		"total": len(items),
	})
}

// ResetQuota clears quota/cooldown routing state for one auth index.
// Optional body field "model" clears only that model via ResetQuotaModel.
func (h *Handler) ResetQuota(c *gin.Context) {
	if h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "core auth manager unavailable"})
		return
	}

	var req struct {
		AuthIndex string `json:"auth_index"`
		Model     string `json:"model"`
	}
	if errBindJSON := c.ShouldBindJSON(&req); errBindJSON != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	authIndex := strings.TrimSpace(req.AuthIndex)
	if authIndex == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "auth_index is required"})
		return
	}
	model := strings.TrimSpace(req.Model)

	auth := h.authByIndex(authIndex)
	if auth == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "auth not found"})
		return
	}

	if model != "" {
		updated, errReset := h.authManager.ResetQuotaModel(c.Request.Context(), auth.ID, model)
		if errReset != nil {
			if errors.Is(errReset, coreauth.ErrModelStateNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": "model not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to reset quota model: %v", errReset)})
			return
		}
		if updated == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "auth not found"})
			return
		}
		updated.EnsureIndex()
		c.JSON(http.StatusOK, gin.H{
			"status":     "ok",
			"auth_index": updated.Index,
			"model":      model,
			"scope":      "model",
		})
		return
	}

	updated, models, errReset := h.authManager.ResetQuota(c.Request.Context(), auth.ID)
	if errReset != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to reset quota: %v", errReset)})
		return
	}
	if updated == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "auth not found"})
		return
	}
	updated.EnsureIndex()

	c.JSON(http.StatusOK, gin.H{
		"status":     "ok",
		"auth_index": updated.Index,
		"models":     models,
		"scope":      "auth",
	})
}

// ResetQuotaAll clears quota/cooldown routing state for all auths, optionally filtered by provider.
// On partial failure it still returns the auths already cleared so operators can see progress.
func (h *Handler) ResetQuotaAll(c *gin.Context) {
	if h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "core auth manager unavailable"})
		return
	}

	var req struct {
		Provider string `json:"provider"`
	}
	// Empty body is allowed.
	if c.Request.ContentLength != 0 {
		if errBindJSON := c.ShouldBindJSON(&req); errBindJSON != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}
	}
	providerFilter := strings.ToLower(strings.TrimSpace(req.Provider))

	auths := h.authManager.List()
	resetAuthIndexes := make([]string, 0)
	modelsCleared := 0
	for _, auth := range auths {
		if auth == nil {
			continue
		}
		if providerFilter != "" && strings.ToLower(strings.TrimSpace(auth.Provider)) != providerFilter {
			continue
		}
		// Skip auths that currently have nothing to clear to keep response focused.
		if !auth.HasCooldownState() {
			continue
		}
		updated, models, errReset := h.authManager.ResetQuota(c.Request.Context(), auth.ID)
		if errReset != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":          fmt.Sprintf("failed to reset quota for auth %s: %v", auth.ID, errReset),
				"auth_index":     auth.Index,
				"status":         "partial",
				"reset_count":    len(resetAuthIndexes),
				"auth_indexes":   resetAuthIndexes,
				"models_cleared": modelsCleared,
				"provider":       strings.TrimSpace(req.Provider),
			})
			return
		}
		if updated == nil {
			continue
		}
		updated.EnsureIndex()
		resetAuthIndexes = append(resetAuthIndexes, updated.Index)
		modelsCleared += len(models)
	}

	c.JSON(http.StatusOK, gin.H{
		"status":         "ok",
		"reset_count":    len(resetAuthIndexes),
		"auth_indexes":   resetAuthIndexes,
		"models_cleared": modelsCleared,
		"provider":       strings.TrimSpace(req.Provider),
	})
}
