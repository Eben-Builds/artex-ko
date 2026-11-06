package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"strings"

	"github.com/Autumn-27/artex/chatoauth"
	"github.com/Autumn-27/norma/llm"
)

// ChatGPT plan tokens are never sent to a user-editable endpoint or proxy.
type chatGPTTransport struct {
	base    http.RoundTripper
	account string
}

func (t chatGPTTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "https" || r.URL.Host != "api.openai.com" || r.URL.Path != "/v1/responses" {
		return nil, errors.New("ChatGPT OAuth only supports the official Responses endpoint")
	}
	r = r.Clone(r.Context())
	r.Header = r.Header.Clone()
	raw, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	r.Body.Close()
	if err != nil {
		return nil, err
	}
	raw, err = chatGPTBody(raw)
	if err != nil {
		return nil, err
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	r.ContentLength = int64(len(raw))
	r.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(raw)), nil }
	token, err := chatoauth.Default().AccessToken(r.Context(), t.account)
	if err != nil {
		return nil, err
	}
	r.Header.Set("Authorization", "Bearer "+token)
	resp, err := t.base.RoundTrip(r)
	if err == nil && resp.StatusCode == 200 {
		resp.Body = &completedStream{body: resp.Body, reader: bufio.NewReader(resp.Body)}
	}
	return resp, err
}
func chatGPTBody(raw []byte) ([]byte, error) {
	var v map[string]any
	if json.Unmarshal(raw, &v) != nil {
		return nil, errors.New("invalid Responses request")
	}
	v["stream"] = true
	v["store"] = false
	for _, key := range []string{"background", "conversation", "max_output_tokens", "max_tool_calls", "metadata", "moderation", "multi_agent", "prompt", "prompt_cache_retention", "safety_identifier", "temperature", "top_logprobs", "top_p", "truncation", "user", "previous_response_id"} {
		delete(v, key)
	}
	if tools, ok := v["tools"].([]any); ok && len(tools) > 0 {
		v["tools"] = []any{map[string]any{"type": "namespace", "name": "artex", "description": "ARTEX local tools", "tools": tools}}
	}
	return json.Marshal(v)
}

// The SDK accepts EOF and incomplete responses; plan usage requires a completed
// terminal event. Inspect one SSE line at a time without buffering the response.
type completedStream struct {
	body     io.ReadCloser
	reader   *bufio.Reader
	buffer   []byte
	complete bool
	failure  error
}

func (s *completedStream) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(s.buffer) > 0 {
		n := copy(p, s.buffer)
		s.buffer = s.buffer[n:]
		return n, nil
	}
	if s.failure != nil {
		return 0, s.failure
	}
	line, err := s.reader.ReadBytes('\n')
	if bytes.HasPrefix(line, []byte("data:")) {
		var frame struct {
			Type     string `json:"type"`
			Response struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			} `json:"response"`
		}
		if json.Unmarshal(bytes.TrimSpace(line[5:]), &frame) == nil {
			switch frame.Type {
			case "response.completed":
				s.complete = true
			case "response.incomplete", "response.failed", "error":
				return 0, fmt.Errorf("ChatGPT inference did not complete (%s, %s)", frame.Type, frame.Response.Error.Code)
			}
		}
	}
	if err != nil {
		if err == io.EOF && !s.complete {
			s.failure = errors.New("ChatGPT stream ended before response.completed")
		} else {
			s.failure = err
		}
	}
	if len(line) == 0 {
		return 0, s.failure
	}
	n := copy(p, line)
	s.buffer = line[n:]
	return n, nil
}
func (s *completedStream) Close() error { return s.body.Close() }

type chatGPTProvider struct{ llm.Provider }

func (p chatGPTProvider) Stream(ctx context.Context, r llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return p.Provider.Stream(ctx, r)
}

// Compaction can call Complete even on streaming profiles. Collect that call
// from SSE as well; the subscription route never accepts stream:false.
func (p chatGPTProvider) Complete(ctx context.Context, r llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	msg := llm.Message{Role: llm.RoleAssistant}
	stop := ""
	var usage llm.Usage
	tool := -1
	for ev, err := range p.Stream(ctx, r) {
		if err != nil {
			return llm.Message{}, "", usage, err
		}
		switch ev.Type {
		case llm.SETextDelta:
			msg.Content = append(msg.Content, llm.TextBlock(ev.Text))
			tool = -1
		case llm.SEThinkingDelta:
			msg.Content = append(msg.Content, llm.ContentBlock{Type: llm.BlockThinking, Thinking: ev.Text})
			tool = -1
		case llm.SEToolUseStart:
			msg.Content = append(msg.Content, llm.ContentBlock{Type: llm.BlockToolUse, ID: ev.ToolID, Name: ev.ToolName})
			tool = len(msg.Content) - 1
		case llm.SEToolInputJSON:
			if tool >= 0 {
				msg.Content[tool].Input = append(msg.Content[tool].Input, []byte(ev.Text)...)
			}
		case llm.SEMessageDelta:
			usage = ev.Usage
			stop = ev.StopReason
		}
	}
	if stop == "" {
		return llm.Message{}, "", usage, errors.New("ChatGPT response has no completion event")
	}
	return msg, stop, usage, nil
}

func isChatGPTKey(key string) bool { return strings.HasPrefix(key, chatoauth.KeyPrefix) }
