// Command fragile runs the Fragile notes MCP server (Phase 0). Given a task,
// it also launches an orchestrator agent to work on it and exits when the
// orchestrator and all its sub-agents are done.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
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
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: fragile [flags] [task]\n\nWith a task, starts an orchestrator agent on it. Without, only serves notes.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if err := run(cfg, strings.Join(flag.Args(), " ")); err != nil {
		log.Fatal(err)
	}
}

func run(cfg notes.Config, task string) error {
	// Agents run in WorkDir, so every path handed to them must be absolute.
	for _, p := range []*string{&cfg.DBPath, &cfg.LogPath, &cfg.AgentDir, &cfg.WorkDir} {
		abs, err := filepath.Abs(*p)
		if err != nil {
			return err
		}
		*p = abs
	}
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

	runner := notes.NewRunner(cfg, store, evlog)
	srv := &notes.Server{Config: cfg, Store: store, Log: evlog, Runner: runner}
	httpSrv := &http.Server{Handler: srv.Handler()}

	// Listen before launching agents so the orchestrator can connect at once.
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return err
	}
	errc := make(chan error, 1)
	go func() { errc <- httpSrv.Serve(ln) }()
	log.Printf("fragile notes server listening on http://%s (session %d)", cfg.Addr, store.SessionID)
	log.Printf("observation log: %s", cfg.LogPath)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var done chan struct{} // stays nil (blocks forever) when only serving
	var orch notes.Agent
	if task != "" {
		if orch, err = runner.StartOrchestrator(task); err != nil {
			return err
		}
		log.Printf("orchestrator agent %d started (pid %d), output: %s", orch.ID, orch.PID, orch.LogPath)
		done = make(chan struct{})
		go func() { runner.Wait(); close(done) }()
	}

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			runner.StopAll()
			return err
		}
	case <-ctx.Done():
		log.Print("interrupted, stopping agents")
		runner.StopAll()
	case <-done:
		log.Print("orchestrator and all sub-agents finished")
		if res := finalResult(orch.LogPath); res != "" {
			fmt.Printf("\n=== ORCHESTRATOR RESULT ===\n%s\n", res)
		}
	}
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return httpSrv.Shutdown(shutCtx)
}

// finalResult returns the text of the last "result" event in a Claude Code
// stream-json log, or "" if there is none.
func finalResult(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	var res string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		var ev struct{ Type, Result string }
		if json.Unmarshal(sc.Bytes(), &ev) == nil && ev.Type == "result" {
			res = ev.Result
		}
	}
	return res
}
