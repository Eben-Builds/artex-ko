package server

import (
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/Autumn-27/artex/chatoauth"
	"github.com/Autumn-27/artex/db"
)

func (s *Server) chatGPTStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	status, err := chatoauth.Default().Status()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, status)
}
func (s *Server) chatGPTStart(w http.ResponseWriter, r *http.Request) {
	// The browser callback must be loopback, never derived from forwarded headers.
	host, port, err := net.SplitHostPort(r.Host)
	if err != nil || (host != "127.0.0.1" && host != "localhost") {
		writeErr(w, 400, "Open ARTEX through http://127.0.0.1:8787 to connect ChatGPT")
		return
	}
	authURL, err := chatoauth.Default().Start("http://127.0.0.1:" + port + "/auth/callback")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]string{"url": authURL})
}
func (s *Server) chatGPTCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if err := chatoauth.Default().Callback(r.Context(), r.URL.Query()); err != nil {
		w.WriteHeader(400)
		fmt.Fprint(w, "ChatGPT sign-in failed. Return to ARTEX > System > LLM for details.")
		return
	}
	fmt.Fprint(w, "ChatGPT connected. Return to ARTEX > System > LLM, select a model, and save. You may close this tab.")
}
func (s *Server) chatGPTModels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	status, err := chatoauth.Default().Status()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	models, err := chatoauth.Default().Models(r.Context(), status.AccountID)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"models": models})
}
func (s *Server) chatGPTConfigure(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	var body struct {
		Model string `json:"model"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	status, err := chatoauth.Default().Status()
	if err != nil || !status.Connected {
		writeErr(w, 400, "Connect a ChatGPT account first")
		return
	}
	models, err := chatoauth.Default().Models(r.Context(), status.AccountID)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	allowed := false
	for _, m := range models {
		if m.ID == body.Model {
			allowed = true
			break
		}
	}
	if !allowed {
		writeErr(w, 400, "Choose a model from this ChatGPT account's catalog")
		return
	}
	key := chatoauth.KeyPrefix + status.AccountID
	p := &db.LLMProfile{Name: "ChatGPT OAuth", Format: "openai-responses", BaseURL: chatoauth.APIBase, APIKey: key, Model: body.Model, Streaming: true, PoolExclude: true, RatePerMinute: 20}
	profiles, err := pg.ListProfiles()
	if err != nil {
		writeErr(w, 500, "Cannot read LLM profiles")
		return
	}
	for _, item := range profiles {
		stored, e := pg.ProfileByID(item.ID)
		if e == nil && stored != nil && stored.APIKey == key {
			p.ID = item.ID
			break
		}
	}
	id, err := pg.SaveProfile(p)
	if err != nil {
		writeErr(w, 500, "Cannot save ChatGPT profile")
		return
	}
	if err = pg.SetActiveProfile(id); err != nil {
		writeErr(w, 500, "Cannot activate ChatGPT profile")
		return
	}
	// Explicitly disable automatic failover: subscription exhaustion must never
	// silently fall through to a separately billed API profile.
	if err = s.m.SetLLMPoolEnabled(false); err != nil {
		writeErr(w, 500, "Cannot disable LLM failover")
		return
	}
	s.invalidateProfileAgents()
	s.reapplyActiveProfile()
	writeJSON(w, 200, map[string]any{"id": id})
}
func (s *Server) chatGPTDisconnect(w http.ResponseWriter, r *http.Request) {
	if err := chatoauth.Default().Disconnect(); err != nil {
		writeErr(w, 500, "Cannot remove ChatGPT credentials")
		return
	}
	s.invalidateProfileAgents()
	s.reapplyActiveProfile()
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// OAuth references must not be reused as bearer credentials by model-list code.
func isChatGPTReference(key string) bool { return strings.HasPrefix(key, chatoauth.KeyPrefix) }
