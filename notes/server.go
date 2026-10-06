// Package notes implements the Fragile notes MCP server: a shared notes board
// that an orchestrator agent and its sub-agents use to coordinate.
//
// It is a library so the Phase 1 desktop app can embed it unchanged; the
// Phase 0 binary in cmd/fragile is a thin wrapper around it.
package notes

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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
	Store  *Store
	Log    *EventLog
	Runner *Runner // launches sub-agents for spawn_subagent
}

// Handler returns the HTTP handler serving the MCP endpoint.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "ok\n") })
	mux.Handle("/mcp/{token}", s.mcpHandler())
	return mux
}

type mcpServerKey struct{}

// requestCtxKey carries the HTTP request's context to tool handlers: the SDK
// detaches the handler's own context from the request, so a client that
// disconnects (or aborts a blocking wait_for_notes) would otherwise go unnoticed.
type requestCtxKey struct{}

// mcpHandler serves one MCP endpoint per agent at /mcp/{token}. The token is a
// random secret handed only to that agent, so a URL cannot be guessed from an
// agent id. The agent is resolved on every request (stateless transport), so
// identity always comes from the URL, and the tool set is built for its role.
func (s *Server) mcpHandler() http.Handler {
	h := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		srv, _ := r.Context().Value(mcpServerKey{}).(*mcp.Server)
		return srv
	}, &mcp.StreamableHTTPOptions{Stateless: true})
	cache := mcp.NewSchemaCache()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent, err := s.Store.GetAgentByToken(r.PathValue("token"))
		if errors.Is(err, ErrNotFound) {
			http.Error(w, "unknown agent", http.StatusNotFound)
			return
		} else if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		srv := s.newMCPServer(agent, cache)
		ctx := context.WithValue(r.Context(), mcpServerKey{}, srv)
		h.ServeHTTP(w, r.WithContext(context.WithValue(ctx, requestCtxKey{}, r.Context())))
	})
}
