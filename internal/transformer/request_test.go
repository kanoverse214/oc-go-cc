package transformer

import (
	"bytes"
	"encoding/json"
	"testing"

	"oc-go-cc/pkg/types"
)

func TestTransformRequestRoundTripReasoning(t *testing.T) {
	deepSeekReasoning := "Let me think step by step"
	openaiResp := &types.ChatCompletionResponse{
		ID:     "resp_123",
		Object: "chat.completion",
		Model:  "deepseek-v4-flash",
		Choices: []types.Choice{{
			Index: 0,
			Message: types.ChatMessage{
				Role:             "assistant",
				Content:          "The answer is 42",
				ReasoningContent: &deepSeekReasoning,
			},
			FinishReason: "stop",
		}},
		Usage: types.UsageInfo{
			PromptTokens:     10,
			CompletionTokens: 20,
		},
	}

	rt := NewResponseTransformer()
	anthropicResp, err := rt.TransformResponse(openaiResp, "deepseek-v4-flash")
	if err != nil {
		t.Fatalf("TransformResponse error: %v", err)
	}

	if len(anthropicResp.Content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(anthropicResp.Content))
	}
	if anthropicResp.Content[0].Type != "thinking" {
		t.Fatalf("expected first block to be thinking, got %s", anthropicResp.Content[0].Type)
	}
	if anthropicResp.Content[0].Thinking != deepSeekReasoning {
		t.Fatalf("thinking text = %q, want %q", anthropicResp.Content[0].Thinking, deepSeekReasoning)
	}

	anthropicReq := &types.MessageRequest{
		Model:     "deepseek-v4-flash",
		MaxTokens: 256,
		Messages: []types.Message{
			{Role: "user", Content: json.RawMessage(`"What is the answer?"`)},
			{
				Role:    "assistant",
				Content: mustJSONBytes(t, anthropicResp.Content),
			},
			{Role: "user", Content: json.RawMessage(`"Explain why?"`)},
		},
	}

	qt := NewRequestTransformer()
	openaiReq, err := qt.TransformRequest(anthropicReq)
	if err != nil {
		t.Fatalf("TransformRequest error: %v", err)
	}

	var assistantMsg *types.ChatMessage
	for i := range openaiReq.Messages {
		if openaiReq.Messages[i].Role == "assistant" {
			assistantMsg = &openaiReq.Messages[i]
			break
		}
	}
	if assistantMsg == nil {
		t.Fatal("assistant message not found in transformed request")
	}

	if assistantMsg.ReasoningContent == nil {
		t.Fatal("ReasoningContent = nil, want non-nil after round-trip")
	}
	if got, want := *assistantMsg.ReasoningContent, deepSeekReasoning; got != want {
		t.Fatalf("ReasoningContent = %q, want %q", got, want)
	}

	body, err := json.Marshal(openaiReq)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}
	if !bytes.Contains(body, []byte(`"reasoning_content"`)) {
		t.Fatalf("serialized request missing reasoning_content field: %s", body)
	}
}

func TestTransformRequestPreservesThinkingAsReasoningContent(t *testing.T) {
	transformer := NewRequestTransformer()
	stream := true

	req := &types.MessageRequest{
		Model:     "claude-test",
		MaxTokens: 256,
		Stream:    &stream,
		Messages: []types.Message{
			{
				Role: "assistant",
				Content: json.RawMessage(`[
					{"type":"thinking","thinking":"Need to look up the weather first","signature":"sig_123"},
					{"type":"tool_use","id":"toolu_123","name":"get_weather","input":{"city":"Kigali"}}
				]`),
			},
		},
	}

	openaiReq, err := transformer.TransformRequest(req)
	if err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}

	if got, want := len(openaiReq.Messages), 1; got != want {
		t.Fatalf("len(Messages) = %d, want %d", got, want)
	}

	msg := openaiReq.Messages[0]
	if got, want := msg.Role, "assistant"; got != want {
		t.Fatalf("Role = %q, want %q", got, want)
	}
	if msg.ReasoningContent == nil {
		t.Fatal("ReasoningContent = nil, want non-nil")
	}
	if got, want := *msg.ReasoningContent, "Need to look up the weather first"; got != want {
		t.Fatalf("ReasoningContent = %q, want %q", got, want)
	}
	if got, want := len(msg.ToolCalls), 1; got != want {
		t.Fatalf("len(ToolCalls) = %d, want %d", got, want)
	}
	if got, want := msg.ToolCalls[0].ID, "toolu_123"; got != want {
		t.Fatalf("ToolCalls[0].ID = %q, want %q", got, want)
	}
	if got, want := msg.ToolCalls[0].Function.Name, "get_weather"; got != want {
		t.Fatalf("ToolCalls[0].Function.Name = %q, want %q", got, want)
	}
	if got, want := msg.ToolCalls[0].Function.Arguments, `{"city":"Kigali"}`; got != want {
		t.Fatalf("ToolCalls[0].Function.Arguments = %q, want %q", got, want)
	}
}

func TestTransformRequestIncludesStreamUsageOptions(t *testing.T) {
	transformer := NewRequestTransformer()
	stream := true

	req := &types.MessageRequest{
		Model:     "deepseek-v4-pro",
		MaxTokens: 256,
		Stream:    &stream,
		Messages: []types.Message{
			{Role: "user", Content: json.RawMessage(`"hello"`)},
		},
	}

	openaiReq, err := transformer.TransformRequest(req)
	if err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}

	if openaiReq.StreamOptions == nil {
		t.Fatal("StreamOptions = nil, want include_usage enabled")
	}
	if !openaiReq.StreamOptions.IncludeUsage {
		t.Fatal("StreamOptions.IncludeUsage = false, want true")
	}
}

func TestTransformRequestOmitsStreamUsageOptionsWhenStreamingDisabled(t *testing.T) {
	transformer := NewRequestTransformer()
	stream := false

	req := &types.MessageRequest{
		Model:     "deepseek-v4-pro",
		MaxTokens: 256,
		Stream:    &stream,
		Messages: []types.Message{
			{Role: "user", Content: json.RawMessage(`"hello"`)},
		},
	}

	openaiReq, err := transformer.TransformRequest(req)
	if err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}

	if openaiReq.StreamOptions != nil {
		t.Fatalf("StreamOptions = %v, want nil when streaming is disabled", openaiReq.StreamOptions)
	}
}

func TestTransformRequestIncludesEmptyReasoningContentForToolCalls(t *testing.T) {
	transformer := NewRequestTransformer()

	req := &types.MessageRequest{
		Model:     "kimi-k2.6",
		MaxTokens: 256,
		Messages: []types.Message{
			{
				Role: "assistant",
				Content: json.RawMessage(`[
					{"type":"tool_use","id":"toolu_456","name":"search_docs","input":{"query":"figma api"}}
				]`),
			},
		},
	}

	openaiReq, err := transformer.TransformRequest(req)
	if err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}

	msg := openaiReq.Messages[0]
	if msg.ReasoningContent == nil {
		t.Fatal("ReasoningContent = nil, want non-nil placeholder")
	}
	if got, want := *msg.ReasoningContent, " "; got != want {
		t.Fatalf("ReasoningContent = %q, want %q", got, want)
	}
}

func TestTransformRequestSerializesAssistantToolCallContent(t *testing.T) {
	transformer := NewRequestTransformer()

	req := &types.MessageRequest{
		Model:     "deepseek-v4-pro",
		MaxTokens: 256,
		Messages: []types.Message{
			{
				Role: "assistant",
				Content: json.RawMessage(`[
					{"type":"tool_use","id":"toolu_456","name":"search_docs","input":{"query":"figma api"}}
				]`),
			},
		},
	}

	openaiReq, err := transformer.TransformRequest(req)
	if err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}

	body, err := json.Marshal(openaiReq)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var payload struct {
		Messages []map[string]json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if _, ok := payload.Messages[0]["content"]; !ok {
		t.Fatalf("serialized assistant tool-call message omitted content: %s", body)
	}
	if got, want := string(payload.Messages[0]["content"]), `""`; got != want {
		t.Fatalf("serialized content = %s, want %s", got, want)
	}
}

func TestTransformRequestAppliesOutputConfigEffort(t *testing.T) {
	transformer := NewRequestTransformer()

	req := &types.MessageRequest{
		Model:     "deepseek-v4-pro",
		MaxTokens: 256,
		Messages: []types.Message{
			{Role: "user", Content: json.RawMessage(`"solve this carefully"`)},
		},
		OutputConfig: &types.OutputConfig{Effort: "high"},
	}

	openaiReq, err := transformer.TransformRequest(req)
	if err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}

	if openaiReq.ReasoningEffort == nil {
		t.Fatal("ReasoningEffort = nil, want high")
	}
	if got, want := *openaiReq.ReasoningEffort, "high"; got != want {
		t.Fatalf("ReasoningEffort = %q, want %q", got, want)
	}
	if got, want := string(openaiReq.Thinking), `{"type":"enabled"}`; got != want {
		t.Fatalf("Thinking = %s, want %s", got, want)
	}
}

func TestTransformRequestAppliesDefaultThinkingWhenHistoryHasThinkingBlocks(t *testing.T) {
	transformer := NewRequestTransformer()

	req := &types.MessageRequest{
		Model:     "deepseek-v4-pro",
		MaxTokens: 256,
		Messages: []types.Message{
			{Role: "user", Content: json.RawMessage(`"solve this carefully"`)},
			{
				Role: "assistant",
				Content: json.RawMessage(`[
					{"type":"thinking","thinking":"Let me think..."},
					{"type":"text","text":"The answer is 42"}
				]`),
			},
		},
	}

	openaiReq, err := transformer.TransformRequest(req)
	if err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}

	if openaiReq.ReasoningEffort == nil {
		t.Fatal("ReasoningEffort = nil, want high (default)")
	}
	if got, want := *openaiReq.ReasoningEffort, "high"; got != want {
		t.Fatalf("ReasoningEffort = %q, want %q", got, want)
	}
	if got, want := string(openaiReq.Thinking), `{"type":"enabled"}`; got != want {
		t.Fatalf("Thinking = %s, want %s", got, want)
	}
}

func TestTransformRequestOmitsThinkingWhenNoOutputConfigAndNoHistory(t *testing.T) {
	transformer := NewRequestTransformer()

	req := &types.MessageRequest{
		Model:     "deepseek-v4-pro",
		MaxTokens: 256,
		Messages: []types.Message{
			{Role: "user", Content: json.RawMessage(`"solve this carefully"`)},
			{Role: "assistant", Content: json.RawMessage(`"The answer is 42"`)},
		},
	}

	openaiReq, err := transformer.TransformRequest(req)
	if err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}

	if openaiReq.ReasoningEffort != nil {
		t.Fatalf("ReasoningEffort = %v, want nil (no output_config, no thinking history)", *openaiReq.ReasoningEffort)
	}
	if openaiReq.Thinking != nil {
		t.Fatalf("Thinking = %s, want nil (no output_config, no thinking history)", string(openaiReq.Thinking))
	}
}

func TestTransformRequestPreservesSystemCacheControl(t *testing.T) {
	transformer := NewRequestTransformer()

	req := &types.MessageRequest{
		Model:     "claude-test",
		MaxTokens: 256,
		System: json.RawMessage(`[
			{"type":"text","text":"You are helpful","cache_control":{"type":"ephemeral"}}
		]`),
		Messages: []types.Message{
			{Role: "user", Content: json.RawMessage(`"hello"`)},
		},
	}

	openaiReq, err := transformer.TransformRequest(req)
	if err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}

	if got, want := len(openaiReq.Messages), 2; got != want {
		t.Fatalf("len(Messages) = %d, want %d", got, want)
	}

	systemMsg := openaiReq.Messages[0]
	if got, want := systemMsg.Role, "system"; got != want {
		t.Fatalf("Messages[0].Role = %q, want %q", got, want)
	}
	if got, want := systemMsg.Content, "You are helpful"; got != want {
		t.Fatalf("Messages[0].Content = %q, want %q", got, want)
	}
	if systemMsg.CacheControl == nil {
		t.Fatal("Messages[0].CacheControl = nil, want non-nil")
	}
	if got, want := systemMsg.CacheControl.Type, "ephemeral"; got != want {
		t.Fatalf("Messages[0].CacheControl.Type = %q, want %q", got, want)
	}
}

func TestTransformRequestOmitsCacheControlWhenAbsent(t *testing.T) {
	transformer := NewRequestTransformer()

	req := &types.MessageRequest{
		Model:     "claude-test",
		MaxTokens: 256,
		System:    json.RawMessage(`"You are helpful"`),
		Messages: []types.Message{
			{Role: "user", Content: json.RawMessage(`"hello"`)},
		},
	}

	openaiReq, err := transformer.TransformRequest(req)
	if err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}

	if got, want := len(openaiReq.Messages), 2; got != want {
		t.Fatalf("len(Messages) = %d, want %d", got, want)
	}

	systemMsg := openaiReq.Messages[0]
	if got, want := systemMsg.Role, "system"; got != want {
		t.Fatalf("Messages[0].Role = %q, want %q", got, want)
	}
	if systemMsg.CacheControl != nil {
		t.Fatalf("Messages[0].CacheControl = %v, want nil", systemMsg.CacheControl)
	}
}

func TestTransformRequestPlacesToolResultsBeforeUserText(t *testing.T) {
	transformer := NewRequestTransformer()

	req := &types.MessageRequest{
		Model:     "claude-test",
		MaxTokens: 256,
		Messages: []types.Message{
			{
				Role: "assistant",
				Content: json.RawMessage(`[
					{"type":"tool_use","id":"toolu_123","name":"create_file","input":{"name":"draft.fig"}}
				]`),
			},
			{
				Role: "user",
				Content: json.RawMessage(`[
					{"type":"tool_result","tool_use_id":"toolu_123","content":"created"},
					{"type":"text","text":"now continue"}
				]`),
			},
		},
	}

	openaiReq, err := transformer.TransformRequest(req)
	if err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}

	if got, want := len(openaiReq.Messages), 3; got != want {
		t.Fatalf("len(Messages) = %d, want %d", got, want)
	}

	if got, want := openaiReq.Messages[0].Role, "assistant"; got != want {
		t.Fatalf("Messages[0].Role = %q, want %q", got, want)
	}
	if got, want := openaiReq.Messages[1].Role, "tool"; got != want {
		t.Fatalf("Messages[1].Role = %q, want %q", got, want)
	}
	if got, want := openaiReq.Messages[1].ToolCallID, "toolu_123"; got != want {
		t.Fatalf("Messages[1].ToolCallID = %q, want %q", got, want)
	}
	if got, want := openaiReq.Messages[2].Role, "user"; got != want {
		t.Fatalf("Messages[2].Role = %q, want %q", got, want)
	}
	if got, want := openaiReq.Messages[2].Content, "now continue"; got != want {
		t.Fatalf("Messages[2].Content = %q, want %q", got, want)
	}
}

func TestTransformRequestOmitsPlaceholderForDeepSeek(t *testing.T) {
	transformer := NewRequestTransformer()

	req := &types.MessageRequest{
		Model:     "deepseek-v4-pro",
		MaxTokens: 256,
		Messages: []types.Message{
			{Role: "user", Content: json.RawMessage(`"hello"`)},
			{
				Role: "assistant",
				Content: json.RawMessage(`[
					{"type":"tool_use","id":"toolu_456","name":"search_docs","input":{"query":"figma api"}}
				]`),
			},
		},
	}

	openaiReq, err := transformer.TransformRequest(req)
	if err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}

	msg := openaiReq.Messages[1]
	if msg.ReasoningContent != nil {
		t.Fatalf("ReasoningContent = %q, want nil (DeepSeek without thinking history should not get placeholder)", *msg.ReasoningContent)
	}
}

func TestTransformRequestDeepSeekPlaceholderWithThinkingHistory(t *testing.T) {
	transformer := NewRequestTransformer()

	req := &types.MessageRequest{
		Model:     "deepseek-v4-flash",
		MaxTokens: 256,
		Messages: []types.Message{
			{Role: "user", Content: json.RawMessage(`"think about this"`)},
			{
				Role: "assistant",
				Content: json.RawMessage(`[
					{"type":"thinking","thinking":"Let me think..."},
					{"type":"text","text":"I considered it"}
				]`),
			},
			{Role: "user", Content: json.RawMessage(`"now use a tool"`)},
			{
				Role: "assistant",
				Content: json.RawMessage(`[
					{"type":"tool_use","id":"toolu_789","name":"search","input":{"q":"test"}}
				]`),
			},
		},
	}

	openaiReq, err := transformer.TransformRequest(req)
	if err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}

	var toolCallAssistant *types.ChatMessage
	for i := range openaiReq.Messages {
		if openaiReq.Messages[i].Role == "assistant" && len(openaiReq.Messages[i].ToolCalls) > 0 {
			toolCallAssistant = &openaiReq.Messages[i]
			break
		}
	}
	if toolCallAssistant == nil {
		t.Fatal("no assistant message with tool_calls found")
	}
	if toolCallAssistant.ReasoningContent == nil {
		t.Fatal("ReasoningContent = nil, want non-nil placeholder for DeepSeek with thinking history")
	}
	if *toolCallAssistant.ReasoningContent != " " {
		t.Fatalf("ReasoningContent = %q, want placeholder space", *toolCallAssistant.ReasoningContent)
	}
}

func mustJSONBytes(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return json.RawMessage(b)
}
