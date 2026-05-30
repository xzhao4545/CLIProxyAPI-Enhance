package handlers

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestBuildOpenAIResponsesStreamErrorChunk(t *testing.T) {
	chunk := BuildOpenAIResponsesStreamErrorChunk(http.StatusInternalServerError, "unexpected EOF", 0)
	var payload map[string]any
	if err := json.Unmarshal(chunk, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["type"] != "error" {
		t.Fatalf("type = %v, want %q", payload["type"], "error")
	}
	if payload["code"] != "internal_server_error" {
		t.Fatalf("code = %v, want %q", payload["code"], "internal_server_error")
	}
	if payload["message"] != "unexpected EOF" {
		t.Fatalf("message = %v, want %q", payload["message"], "unexpected EOF")
	}
	if payload["sequence_number"] != float64(0) {
		t.Fatalf("sequence_number = %v, want %v", payload["sequence_number"], 0)
	}
}

func TestBuildOpenAIResponsesStreamErrorChunkExtractsHTTPErrorBody(t *testing.T) {
	chunk := BuildOpenAIResponsesStreamErrorChunk(
		http.StatusInternalServerError,
		`{"error":{"message":"oops","type":"server_error","code":"internal_server_error"}}`,
		0,
	)
	var payload map[string]any
	if err := json.Unmarshal(chunk, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["type"] != "error" {
		t.Fatalf("type = %v, want %q", payload["type"], "error")
	}
	if payload["code"] != "internal_server_error" {
		t.Fatalf("code = %v, want %q", payload["code"], "internal_server_error")
	}
	if payload["message"] != "oops" {
		t.Fatalf("message = %v, want %q", payload["message"], "oops")
	}
}

func TestBuildOpenAIResponsesStreamFailedChunk(t *testing.T) {
	chunk := BuildOpenAIResponsesStreamFailedChunk(
		http.StatusTooManyRequests,
		`{"error":{"message":"keyword filter matched","type":"rate_limit_error","code":"keyword_filtered"}}`,
		3,
	)
	var payload struct {
		Type           string `json:"type"`
		SequenceNumber int    `json:"sequence_number"`
		Response       struct {
			Object string `json:"object"`
			Status string `json:"status"`
			Error  struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		} `json:"response"`
	}
	if err := json.Unmarshal(chunk, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Type != "response.failed" {
		t.Fatalf("type = %q, want response.failed", payload.Type)
	}
	if payload.SequenceNumber != 3 {
		t.Fatalf("sequence_number = %d, want 3", payload.SequenceNumber)
	}
	if payload.Response.Object != "response" || payload.Response.Status != "failed" {
		t.Fatalf("response = object %q status %q, want response failed", payload.Response.Object, payload.Response.Status)
	}
	if payload.Response.Error.Code != "keyword_filtered" {
		t.Fatalf("error code = %q, want keyword_filtered", payload.Response.Error.Code)
	}
	if payload.Response.Error.Message != "keyword filter matched" {
		t.Fatalf("error message = %q, want keyword filter matched", payload.Response.Error.Message)
	}
}
