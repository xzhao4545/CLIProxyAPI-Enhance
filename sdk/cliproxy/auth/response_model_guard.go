package auth

import (
	"context"
	"strings"
	"sync"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/thinking"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
)

const maxValidatedStreamBytes = 64 << 20

func (m *Manager) validatesResponseModel(provider, model string, auth *Auth) bool {
	cfg := m.runtimeConfigSnapshot()
	if cfg == nil {
		return false
	}
	baseModel := strings.TrimSpace(thinking.ParseSuffix(model).ModelName)
	for _, rule := range cfg.ResponseModelValidation {
		if !strings.EqualFold(strings.TrimSpace(rule.Provider), strings.TrimSpace(provider)) {
			continue
		}
		if label := strings.TrimSpace(rule.Label); label != "" && (auth == nil || label != strings.TrimSpace(auth.Label)) {
			continue
		}
		for _, configuredModel := range rule.Models {
			if strings.EqualFold(strings.TrimSpace(configuredModel), baseModel) {
				return true
			}
		}
	}
	return false
}

// bufferWebsocketObservations keeps response callbacks behind the same validation barrier as chunks.
func bufferWebsocketObservations(opts cliproxyexecutor.Options) (cliproxyexecutor.Options, func()) {
	observer := opts.WebSocketResponseObserver
	if observer == nil {
		return opts, func() {}
	}
	type observation struct {
		ctx   context.Context
		event cliproxyexecutor.WebSocketResponseEvent
	}
	var mu sync.Mutex
	var pending []observation
	opts.WebSocketResponseObserver = func(ctx context.Context, event cliproxyexecutor.WebSocketResponseEvent) {
		event.Payload = append([]byte(nil), event.Payload...)
		mu.Lock()
		pending = append(pending, observation{ctx: ctx, event: event})
		mu.Unlock()
	}
	return opts, func() {
		mu.Lock()
		events := pending
		pending = nil
		mu.Unlock()
		for _, item := range events {
			observer(item.ctx, item.event)
		}
	}
}

func (m *Manager) executeWithResponseModelValidation(ctx context.Context, executor ProviderExecutor, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	if !m.validatesResponseModel(executor.Identifier(), req.Model, auth) {
		return executor.Execute(ctx, auth, req, opts)
	}
	execCtx, guard := usage.WithResponseModelGuard(ctx, executor.Identifier(), req.Model)
	execOpts, releaseObservations := bufferWebsocketObservations(opts)
	response, errExecute := executor.Execute(execCtx, auth, req, execOpts)
	if errValidate := guard.Finish(errExecute); errValidate != nil {
		return cliproxyexecutor.Response{}, errValidate
	}
	releaseObservations()
	return response, nil
}

func (m *Manager) executeStreamWithResponseModelValidation(ctx context.Context, executor ProviderExecutor, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	if !m.validatesResponseModel(executor.Identifier(), req.Model, auth) {
		return executor.ExecuteStream(ctx, auth, req, opts)
	}
	// The internal consumer drains upstream before the HTTP consumer can acknowledge delivery.
	execCtx, cancel := context.WithCancel(usage.WithoutStreamDelivery(ctx))
	defer cancel()
	execCtx, guard := usage.WithResponseModelGuard(execCtx, executor.Identifier(), req.Model)
	execOpts, releaseObservations := bufferWebsocketObservations(opts)
	stream, errStream := executor.ExecuteStream(execCtx, auth, req, execOpts)
	stream, errStream = validateStreamResult(stream, errStream)
	if errStream != nil {
		return nil, guard.Finish(errStream)
	}
	defer discardStreamChunks(stream.Chunks)
	var buffered []cliproxyexecutor.StreamChunk
	var bytesBuffered int
	for {
		select {
		case <-ctx.Done():
			return nil, guard.Finish(ctx.Err())
		case chunk, ok := <-stream.Chunks:
			if !ok {
				if errValidate := guard.Finish(nil); errValidate != nil {
					return nil, errValidate
				}
				releaseObservations()
				chunks := make(chan cliproxyexecutor.StreamChunk, len(buffered))
				for _, item := range buffered {
					chunks <- item
				}
				close(chunks)
				return &cliproxyexecutor.StreamResult{Headers: stream.Headers, Chunks: chunks}, nil
			}
			if chunk.Err != nil {
				return nil, guard.Finish(chunk.Err)
			}
			bytesBuffered += len(chunk.Payload)
			if bytesBuffered > maxValidatedStreamBytes {
				return nil, guard.Finish(NewRequestScopedError("response-model validation stream buffer exceeds 64 MiB", 502))
			}
			chunk.Payload = append([]byte(nil), chunk.Payload...)
			buffered = append(buffered, chunk)
		}
	}
}
