package server

import (
	"net/http/httptest"
	"testing"
)

func TestChatGPTOAuthManagementRequiresAdmin(t *testing.T) {
	s := &Server{}
	h := s.Handler()
	for _, entry := range []struct{ method, path string }{
		{"GET", "/api/llm/oauth/chatgpt"},
		{"POST", "/api/llm/oauth/chatgpt/start"},
		{"GET", "/api/llm/oauth/chatgpt/models"},
		{"POST", "/api/llm/oauth/chatgpt/configure"},
		{"DELETE", "/api/llm/oauth/chatgpt"},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(entry.method, entry.path, nil))
		if w.Code != 401 {
			t.Fatalf("%s %s returned %d", entry.method, entry.path, w.Code)
		}
	}
}
func TestChatGPTOAuthStartRejectsRemoteHost(t *testing.T) {
	s := &Server{}
	for _, host := range []string{"https://public.example", "http://127.0.0.1.attacker.invalid:8787", "http://[::1]:8787"} {
		w := httptest.NewRecorder()
		s.chatGPTStart(w, httptest.NewRequest("POST", host+"/api/llm/oauth/chatgpt/start", nil))
		if w.Code != 400 {
			t.Fatalf("remote host accepted: %s", host)
		}
	}
}
