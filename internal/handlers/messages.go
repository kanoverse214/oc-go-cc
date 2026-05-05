// Package handlers contains HTTP request handlers for API endpoints.
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"oc-go-cc/internal/client"
	"oc-go-cc/internal/metrics"
	"oc-go-cc/internal/middleware"
	"oc-go-cc/internal/transformer"
	"oc-go-cc/pkg/types"
)

// MessagesHandler handles /v1/messages requests.
type MessagesHandler struct {
	client              *client.OpenCodeClient
	requestTransformer  *transformer.RequestTransformer
	responseTransformer *transformer.ResponseTransformer
	streamHandler       *transformer.StreamHandler
	logger              *slog.Logger
	rateLimiter         *middleware.RateLimiter
	requestDedup        *middleware.RequestDeduplicator
	requestIDGen        *middleware.RequestIDGenerator
	metrics             *metrics.Metrics
}

// responseWriter wraps http.ResponseWriter to track if headers were written.
type responseWriter struct {
	http.ResponseWriter
	wroteHeader bool
}

func (w *responseWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.wroteHeader = true
		w.ResponseWriter.WriteHeader(code)
	}
}

func (w *responseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

// Flush implements http.Flusher for SSE streaming support.
func (w *responseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// NewMessagesHandler creates a new messages handler.
func NewMessagesHandler(
	openCodeClient *client.OpenCodeClient,
	metrics *metrics.Metrics,
) *MessagesHandler {
	return &MessagesHandler{
		client:              openCodeClient,
		requestTransformer:  transformer.NewRequestTransformer(),
		responseTransformer: transformer.NewResponseTransformer(),
		streamHandler:       transformer.NewStreamHandler(),
		logger:              slog.Default(),
		rateLimiter:         middleware.NewRateLimiter(100, time.Minute),
		requestDedup:        middleware.NewRequestDeduplicator(500 * time.Millisecond),
		requestIDGen:        middleware.NewRequestIDGenerator(),
		metrics:             metrics,
	}
}

// HandleMessages handles POST /v1/messages.
func (h *MessagesHandler) HandleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Generate or get request ID for correlation
	requestID := r.Header.Get("X-Request-ID")
	if requestID == "" {
		requestID = h.requestIDGen.Generate()
	}
	w.Header().Set("X-Request-ID", requestID)

	// Rate limiting
	clientIP := middleware.GetClientIP(r)
	if !h.rateLimiter.Allow(clientIP) {
		h.metrics.RecordRateLimited()
		h.logger.Warn("rate limited", "client", clientIP, "request_id", requestID)
		http.Error(w, "rate limited", http.StatusTooManyRequests)
		return
	}

	// Read the raw request body
	var rawBody json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&rawBody); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid request body", err)
		return
	}

	// Deduplicate - skip duplicate requests
	if _, ok := h.requestDedup.TryAcquire(rawBody); !ok {
		h.metrics.RecordDeduplicated()
		h.logger.Info("duplicate request skipped", "request_id", requestID)
		return
	}

	// Parse into Anthropic request
	var anthropicReq types.MessageRequest
	if err := json.Unmarshal(rawBody, &anthropicReq); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid request body", err)
		return
	}

	// Validate request
	if err := anthropicReq.Validate(); err != nil {
		h.sendError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}

	// Record metrics
	isStreaming := anthropicReq.Stream != nil && *anthropicReq.Stream
	h.metrics.RecordRequest(isStreaming)

	modelID := anthropicReq.Model

	h.logger.Info("received request",
		"model", modelID,
		"streaming", isStreaming,
		"messages", len(anthropicReq.Messages),
		"tools", len(anthropicReq.Tools),
		"max_tokens", anthropicReq.MaxTokens,
	)

	if isStreaming {
		h.handleStreaming(w, r, &anthropicReq, modelID, rawBody)
	} else {
		h.handleNonStreaming(w, r, &anthropicReq, modelID, rawBody)
	}
}

// handleStreaming handles a streaming request with real-time SSE proxying.
func (h *MessagesHandler) handleStreaming(
	w http.ResponseWriter,
	r *http.Request,
	anthropicReq *types.MessageRequest,
	modelID string,
	rawBody json.RawMessage,
) {
	clientCtx := r.Context()
	rw := &responseWriter{ResponseWriter: w}

	// Set SSE headers immediately so Claude Code knows the stream is alive.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	// Start heartbeat to keep connection alive while waiting for upstream.
	heartbeatDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_, _ = fmt.Fprintf(w, ":keepalive\n\n")
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			case <-heartbeatDone:
				return
			case <-clientCtx.Done():
				return
			}
		}
	}()
	defer close(heartbeatDone)

	streamStart := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	h.logger.Info("streaming request", "model", modelID)

	// Anthropic-native model (MiniMax): send raw Anthropic request
	if client.IsAnthropicModel(modelID) {
		modelBody := replaceModelInRawBody(rawBody, modelID)
		if err := h.handleAnthropicStreaming(ctx, rw, modelBody, modelID); err != nil {
			if clientCtx.Err() == context.Canceled {
				h.logger.Info("client disconnected during anthropic stream")
				return
			}
			h.logger.Error("anthropic streaming failed", "model", modelID, "error", err)
			h.metrics.RecordFailure()
			h.sendStreamError(rw, "upstream streaming failed")
			return
		}
		latency := time.Since(streamStart)
		h.metrics.RecordSuccess(modelID, latency)
		return
	}

	// OpenAI-compatible model: transform and stream
	openaiReq, err := h.requestTransformer.TransformRequest(anthropicReq)
	if err != nil {
		h.logger.Error("request transform failed", "model", modelID, "error", err)
		h.metrics.RecordFailure()
		h.sendStreamError(rw, "request transform failed")
		return
	}

	streamBody, err := h.client.GetStreamingBody(ctx, modelID, openaiReq)
	if err != nil {
		if clientCtx.Err() == context.Canceled {
			h.logger.Info("client disconnected during upstream request")
			return
		}
		h.logger.Error("streaming request failed", "model", modelID, "error", err)
		h.metrics.RecordFailure()
		h.sendStreamError(rw, "upstream streaming failed")
		return
	}
	defer streamBody.Close()

	if err := h.streamHandler.ProxyStream(rw, streamBody, modelID, clientCtx); err != nil {
		if err == transformer.ErrClientDisconnected || clientCtx.Err() == context.Canceled {
			h.logger.Info("client disconnected during stream")
			return
		}
		h.logger.Error("stream proxy failed", "model", modelID, "error", err)
		h.metrics.RecordFailure()
		return
	}

	latency := time.Since(streamStart)
	h.metrics.RecordSuccess(modelID, latency)
	h.logger.Info("streaming completed", "model", modelID, "latency", latency)
}

// replaceModelInRawBody replaces the model field in raw JSON body with the actual model ID.
// This is needed for Anthropic endpoint which validates the model name.
func replaceModelInRawBody(rawBody json.RawMessage, modelID string) json.RawMessage {
	// Simple string replacement - find "model":"..." and replace with "model":"actual-model"
	bodyStr := string(rawBody)

	// Try to find and replace the model field
	// Pattern: "model":"claude-..." or "model":"any-model-name"
	if idx := strings.Index(bodyStr, `"model":"`); idx != -1 {
		start := idx + len(`"model":"`)
		if end := strings.Index(bodyStr[start:], `"`); end != -1 {
			oldModel := bodyStr[start : start+end]
			// Replace the model value
			newBody := bodyStr[:start] + modelID + bodyStr[start+end:]
			slog.Debug("replaced model in request body",
				"old_model", oldModel,
				"new_model", modelID,
				"success", true)
			return json.RawMessage(newBody)
		}
	}

	slog.Warn("could not find model field in request body, using original",
		"body_preview", bodyStr[:min(len(bodyStr), 200)])
	// If we couldn't parse, return original (will likely fail upstream but that's ok)
	return rawBody
}

// handleAnthropicStreaming sends a raw Anthropic request to the Anthropic endpoint.
func (h *MessagesHandler) handleAnthropicStreaming(
	ctx context.Context,
	w http.ResponseWriter,
	rawBody json.RawMessage,
	modelID string,
) error {
	// Debug: Log what we're sending
	h.logger.Debug("sending anthropic streaming request",
		"model_id", modelID,
		"body_preview", string(rawBody)[:min(len(rawBody), 200)])

	// Send raw Anthropic request to Anthropic endpoint
	// Use ctx so cancellation propagates when client disconnects
	resp, err := h.client.SendAnthropicRequest(ctx, rawBody, true)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	// Copy the response directly (already in Anthropic format)
	// SSE headers already set by handleStreaming
	// Use io.Copy which handles streaming efficiently
	_, err = io.Copy(w, resp.Body)
	if err != nil {
		// Check if this was a client disconnect
		if ctx.Err() == context.Canceled {
			return transformer.ErrClientDisconnected
		}
		return fmt.Errorf("failed to copy response: %w", err)
	}

	return nil
}

// sendStreamError sends an error event in the SSE stream.
// Use this when headers have already been written.
func (h *MessagesHandler) sendStreamError(w http.ResponseWriter, message string) {
	h.logger.Error("sending stream error", "message", message)

	errorEvent := map[string]interface{}{
		"type": "error",
		"error": map[string]interface{}{
			"type":    "api_error",
			"message": message,
		},
	}

	data, _ := json.Marshal(errorEvent)
	_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", string(data))

	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// handleNonStreaming handles a non-streaming request.
func (h *MessagesHandler) handleNonStreaming(
	w http.ResponseWriter,
	r *http.Request,
	anthropicReq *types.MessageRequest,
	modelID string,
	rawBody json.RawMessage,
) {
	ctx := r.Context()
	startTime := time.Now()

	var responseBody []byte
	var err error

	if client.IsAnthropicModel(modelID) {
		responseBody, err = h.executeAnthropicRequest(ctx, rawBody, modelID)
	} else {
		responseBody, err = h.executeOpenAIRequest(ctx, anthropicReq, modelID)
	}

	if err != nil {
		h.metrics.RecordFailure()
		h.sendError(w, http.StatusBadGateway, "upstream request failed", err)
		return
	}

	latency := time.Since(startTime)
	h.metrics.RecordSuccess(modelID, latency)

	h.logger.Info("request completed",
		"model", modelID,
		"latency", latency,
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(responseBody)
}

// executeAnthropicRequest executes a request to the Anthropic endpoint (for MiniMax models).
func (h *MessagesHandler) executeAnthropicRequest(
	ctx context.Context,
	rawBody json.RawMessage,
	modelID string,
) ([]byte, error) {
	modelBody := replaceModelInRawBody(rawBody, modelID)
	resp, err := h.client.SendAnthropicRequest(ctx, modelBody, false)
	if err != nil {
		return nil, fmt.Errorf("anthropic request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return body, nil
}

// executeOpenAIRequest executes a request to the OpenAI endpoint with transformation.
func (h *MessagesHandler) executeOpenAIRequest(
	ctx context.Context,
	anthropicReq *types.MessageRequest,
	modelID string,
) ([]byte, error) {
	openaiReq, err := h.requestTransformer.TransformRequest(anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("request transform failed: %w", err)
	}

	resp, err := h.client.ChatCompletionNonStreaming(ctx, modelID, openaiReq)
	if err != nil {
		return nil, fmt.Errorf("chat completion failed: %w", err)
	}

	anthropicResp, err := h.responseTransformer.TransformResponse(resp, modelID)
	if err != nil {
		return nil, fmt.Errorf("response transform failed: %w", err)
	}

	return json.Marshal(anthropicResp)
}

// sendError sends an error response in Anthropic format.
// Safe to call multiple times - subsequent calls are no-ops.
func (h *MessagesHandler) sendError(w http.ResponseWriter, statusCode int, message string, err error) {
	h.logger.Error("request error",
		"status", statusCode,
		"message", message,
		"error", err,
	)

	// Use the wrapped writer if available to prevent duplicate WriteHeader calls
	if rw, ok := w.(*responseWriter); ok && rw.wroteHeader {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	errorResp := transformer.TransformErrorResponse(statusCode, message)
	_ = json.NewEncoder(w).Encode(errorResp)
}
