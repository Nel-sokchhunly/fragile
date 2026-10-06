// Package notes implements the Fragile notes MCP server: a shared notes board
// that an orchestrator agent and its sub-agents use to coordinate.
//
// It is a library so the Phase 1 desktop app can embed it unchanged; the
// Phase 0 binary in cmd/fragile is a thin wrapper around it.
package notes

import (
	"io"
	"net/http"
)

// Config holds the paths and settings the server needs.
type Config struct {
	Addr     string // listen address, e.g. "127.0.0.1:7777"
	DBPath   string // SQLite database file
	LogPath  string // observation log (JSONL), meant for `tail -f`
	AgentDir string // per-agent output logs and MCP configs
	WorkDir  string // working directory agents run in
}

// Server is the notes server. Store and Log are shared by all tool handlers.
type Server struct {
	Config Config
	Store  *Store
	Log    *EventLog
}

// Handler returns the HTTP handler serving the MCP endpoint.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "ok\n") })
	return mux
}
