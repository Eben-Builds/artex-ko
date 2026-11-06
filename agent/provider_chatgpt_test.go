package agent

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/chatoauth"
)

func TestChatGPTRequestUsesPlanProtocol(t *testing.T) {
	raw, err := chatGPTBody([]byte(`{"model":"selected-model","input":[{"role":"user","content":"test"}],"tools":[{"type":"function","name":"test_tool"}],"max_output_tokens":8192,"temperature":1,"store":true,"stream":false,"previous_response_id":"r"}`))
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	json.Unmarshal(raw, &body)
	if body["store"] != false || body["stream"] != true || body["max_output_tokens"] != nil || body["temperature"] != nil || body["previous_response_id"] != nil {
		t.Fatalf("wrong protocol %s", raw)
	}
	tools := body["tools"].([]any)
	if tools[0].(map[string]any)["type"] != "namespace" {
		t.Fatal("tools were not namespaced")
	}
}
func TestChatGPTStreamRequiresCompletedTerminalEvent(t *testing.T) {
	for _, kind := range []string{"completed", "incomplete", "failed", "truncated"} {
		t.Run(kind, func(t *testing.T) {
			body := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"OK\"}\n\n"
			if kind != "truncated" {
				body += "data: {\"type\":\"response." + kind + "\"}\n\n"
			}
			stream := &completedStream{body: io.NopCloser(strings.NewReader(body)), reader: bufio.NewReader(strings.NewReader(body))}
			_, err := io.ReadAll(stream)
			if (err == nil) != (kind == "completed") {
				t.Fatalf("%s: %v", kind, err)
			}
		})
	}
}
func TestChatGPTCannotSendTokenToCustomEndpoint(t *testing.T) {
	for _, base := range []string{"https://attacker.invalid/v1", "http://api.openai.com/v1"} {
		cfg := ConfigFrom("openai-responses", "selected-model", base, chatoauth.KeyPrefix+"test", "")
		if _, err := cfg.NewProvider(); err == nil {
			t.Fatal("custom endpoint accepted", base)
		}
	}
	cfg := ConfigFrom("openai-responses", "selected-model", chatoauth.APIBase, chatoauth.KeyPrefix+"test", "http://127.0.0.1:9999")
	if _, err := cfg.NewProvider(); err == nil {
		t.Fatal("custom proxy accepted")
	}
}
