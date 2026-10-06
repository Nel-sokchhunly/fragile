// Command fragile runs the Fragile notes MCP server (Phase 0).
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/Nel-sokchhunly/fragile/notes"
)

func main() {
	var cfg notes.Config
	flag.StringVar(&cfg.Addr, "addr", "127.0.0.1:7777", "listen address (keep it on localhost)")
	flag.StringVar(&cfg.DBPath, "db", ".fragile/fragile.db", "SQLite database path")
	flag.StringVar(&cfg.LogPath, "log", ".fragile/events.jsonl", "observation log path (tail -f it)")
	flag.StringVar(&cfg.AgentDir, "agent-dir", ".fragile/agents", "per-agent output logs and MCP configs")
	flag.StringVar(&cfg.WorkDir, "dir", ".", "working directory agents run in")
	flag.Parse()

	if err := run(cfg); err != nil {
		log.Fatal(err)
	}
}

func run(cfg notes.Config) error {
	for _, dir := range []string{filepath.Dir(cfg.DBPath), filepath.Dir(cfg.LogPath), cfg.AgentDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	store, err := notes.OpenStore(cfg.DBPath)
	if err != nil {
		return err
	}
	defer store.Close()
	evlog, err := notes.OpenEventLog(cfg.LogPath)
	if err != nil {
		return err
	}
	defer evlog.Close()

	srv := &notes.Server{Config: cfg, Store: store, Log: evlog}
	httpSrv := &http.Server{Addr: cfg.Addr, Handler: srv.Handler()}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	errc := make(chan error, 1)
	go func() { errc <- httpSrv.ListenAndServe() }()
	log.Printf("fragile notes server listening on http://%s (session %d)", cfg.Addr, store.SessionID)

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Print("shutting down")
	}
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return httpSrv.Shutdown(shutCtx)
}
