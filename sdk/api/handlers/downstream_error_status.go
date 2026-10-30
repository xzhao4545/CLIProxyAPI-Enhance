package handlers

import (
	"errors"
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/interfaces"
)

// DownstreamErrorMessage replaces only a disclosed upstream 4xx status.
// It leaves the original message and error available to usage and lifecycle hooks.
func (h *BaseAPIHandler) DownstreamErrorMessage(msg *interfaces.ErrorMessage) *interfaces.ErrorMessage {
	if h == nil || h.Cfg == nil || !h.Cfg.Upstream4xxTo502 || msg == nil || msg.DirectResponse {
		return msg
	}
	var upstream interface{ UpstreamStatusCode() int }
	if !errors.As(msg.Error, &upstream) {
		return msg
	}
	status := upstream.UpstreamStatusCode()
	if status < 400 || status >= 500 {
		return msg
	}
	downstream := *msg
	downstream.StatusCode = http.StatusBadGateway
	return &downstream
}
