package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type openAIResponsesStreamErrorChunk struct {
	Type           string `json:"type"`
	Code           string `json:"code"`
	Message        string `json:"message"`
	SequenceNumber int    `json:"sequence_number"`
}

type openAIResponsesStreamFailedChunk struct {
	Type           string                        `json:"type"`
	SequenceNumber int                           `json:"sequence_number"`
	Response       openAIResponsesFailedResponse `json:"response"`
}

type openAIResponsesFailedResponse struct {
	ID        string                      `json:"id"`
	Object    string                      `json:"object"`
	CreatedAt int64                       `json:"created_at"`
	Status    string                      `json:"status"`
	Error     openAIResponsesFailureError `json:"error"`
	Output    []any                       `json:"output"`
	Usage     any                         `json:"usage"`
}

type openAIResponsesFailureError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func openAIResponsesStreamErrorCode(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return "invalid_api_key"
	case http.StatusForbidden:
		return "insufficient_quota"
	case http.StatusTooManyRequests:
		return "rate_limit_exceeded"
	case http.StatusNotFound:
		return "model_not_found"
	case http.StatusRequestTimeout:
		return "request_timeout"
	default:
		if status >= http.StatusInternalServerError {
			return "internal_server_error"
		}
		if status >= http.StatusBadRequest {
			return "invalid_request_error"
		}
		return "unknown_error"
	}
}

func openAIResponsesStreamErrorFields(status int, errText string, sequenceNumber int) (int, string, string, int) {
	if status <= 0 {
		status = http.StatusInternalServerError
	}
	if sequenceNumber < 0 {
		sequenceNumber = 0
	}

	message := strings.TrimSpace(errText)
	if message == "" {
		message = http.StatusText(status)
	}

	code := openAIResponsesStreamErrorCode(status)

	trimmed := strings.TrimSpace(errText)
	if trimmed != "" && json.Valid([]byte(trimmed)) {
		var payload map[string]any
		if err := json.Unmarshal([]byte(trimmed), &payload); err == nil {
			if t, ok := payload["type"].(string); ok && strings.TrimSpace(t) == "error" {
				if m, ok := payload["message"].(string); ok && strings.TrimSpace(m) != "" {
					message = strings.TrimSpace(m)
				}
				if v, ok := payload["code"]; ok && v != nil {
					if c, ok := v.(string); ok && strings.TrimSpace(c) != "" {
						code = strings.TrimSpace(c)
					} else {
						code = strings.TrimSpace(fmt.Sprint(v))
					}
				}
				if v, ok := payload["sequence_number"].(float64); ok && sequenceNumber == 0 {
					sequenceNumber = int(v)
				}
			}
			if e, ok := payload["error"].(map[string]any); ok {
				if m, ok := e["message"].(string); ok && strings.TrimSpace(m) != "" {
					message = strings.TrimSpace(m)
				}
				if v, ok := e["code"]; ok && v != nil {
					if c, ok := v.(string); ok && strings.TrimSpace(c) != "" {
						code = strings.TrimSpace(c)
					} else {
						code = strings.TrimSpace(fmt.Sprint(v))
					}
				}
			}
		}
	}

	if strings.TrimSpace(code) == "" {
		code = "unknown_error"
	}
	return status, code, message, sequenceNumber
}

// BuildOpenAIResponsesStreamErrorChunk builds an OpenAI Responses streaming error chunk.
//
// Important: OpenAI's HTTP error bodies are shaped like {"error":{...}}; those are valid for
// non-streaming responses, but streaming clients validate SSE `data:` payloads against a union
// of chunks that requires a top-level `type` field.
func BuildOpenAIResponsesStreamErrorChunk(status int, errText string, sequenceNumber int) []byte {
	_, code, message, sequenceNumber := openAIResponsesStreamErrorFields(status, errText, sequenceNumber)

	data, err := json.Marshal(openAIResponsesStreamErrorChunk{
		Type:           "error",
		Code:           code,
		Message:        message,
		SequenceNumber: sequenceNumber,
	})
	if err == nil {
		return data
	}

	// Extremely defensive fallback.
	data, _ = json.Marshal(openAIResponsesStreamErrorChunk{
		Type:           "error",
		Code:           "internal_server_error",
		Message:        message,
		SequenceNumber: sequenceNumber,
	})
	if len(data) > 0 {
		return data
	}
	return []byte(`{"type":"error","code":"internal_server_error","message":"internal error","sequence_number":0}`)
}

// BuildOpenAIResponsesStreamFailedChunk builds a Responses-native failure event.
// It complements the generic `error` event so clients that follow the Responses
// lifecycle can surface a terminal model failure.
func BuildOpenAIResponsesStreamFailedChunk(status int, errText string, sequenceNumber int) []byte {
	_, code, message, sequenceNumber := openAIResponsesStreamErrorFields(status, errText, sequenceNumber)
	data, err := json.Marshal(openAIResponsesStreamFailedChunk{
		Type:           "response.failed",
		SequenceNumber: sequenceNumber,
		Response: openAIResponsesFailedResponse{
			ID:        "",
			Object:    "response",
			CreatedAt: 0,
			Status:    "failed",
			Error: openAIResponsesFailureError{
				Code:    code,
				Message: message,
			},
			Output: []any{},
			Usage:  nil,
		},
	})
	if err == nil {
		return data
	}
	return []byte(`{"type":"response.failed","sequence_number":0,"response":{"id":"","object":"response","created_at":0,"status":"failed","error":{"code":"internal_server_error","message":"internal error"},"output":[],"usage":null}}`)
}
