# Fragile

Agile, for agents: an orchestrator agent spawns sub-agents that coordinate through a shared notes board. See [fragile-spec.md](fragile-spec.md).

**Current phase: 0 — notes server experiment.** A single Go binary hosts the notes MCP server and launches headless Claude Code agents. No UI.

## Build

```bash
go build -o fragile ./cmd/fragile
```

Requires Go and the `claude` CLI on `PATH`, logged in.

## Run

```bash
./fragile -dir /path/to/repo "the task for the orchestrator"
```

This starts the notes server on `127.0.0.1:7777`, launches the orchestrator in `-dir`, and exits once the orchestrator and all its sub-agents have finished (Ctrl-C stops everything). Without a task it only serves notes.

Watch what the agents do:

```bash
tail -f .fragile/events.jsonl
```

| Path (defaults) | Contents |
|---|---|
| `.fragile/fragile.db` | SQLite: sessions, agents, tasks, boards, notes, escalations |
| `.fragile/events.jsonl` | Observation log: notes, spawns, status changes, escalations |
| `.fragile/agents/agent-<id>.jsonl` | Raw stream-json output of each agent |

Run `./fragile -h` for all flags.

## How it works

- Each agent connects to `http://127.0.0.1:7777/mcp/<token>`, where the token is a random secret generated for that agent; the server attributes every note to that agent. `-addr` must be a loopback address, since the server can launch agents.
- All agents get `read_notes`, `post_note`, `update_note`. Only the orchestrator gets `spawn_subagent`, `get_subagent_status`, `escalate_to_user`.
- Sub-agents are separate `claude -p` processes started by `spawn_subagent`. Claude Code's built-in sub-agent tools (`Task`, `Agent`, `Workflow`) are disallowed for every agent.
- `escalate_to_user` prints a marked banner to stdout and logs it; there is no way to answer in Phase 0.
- Known limit: all agents run as your OS user with Bash, so a sub-agent could read another agent's token from its `agent-<id>.mcp.json` in the agent dir (kept `0700`/`0600`) and impersonate it. Real isolation needs a sandbox (future phase).
- Prompts live in [notes/prompts](notes/prompts).

## Desktop app (Phase 1)

The desktop app lives in [app/](app) (Wails v2, React + TypeScript + Tailwind/shadcn, pnpm). It is part of the same Go module. Requires the [Wails CLI](https://wails.io/docs/gettingstarted/installation) (`go install github.com/wailsapp/wails/v2/cmd/wails@v2.14.0`), Node, and pnpm.

```bash
cd app
wails dev     # live-reload dev window
wails build   # production build: app/build/bin/Fragile.app on macOS
```

Backend state reaches the UI as Wails events into Zustand stores (`app/frontend/src/lib/events.ts`, `app/frontend/src/store`); components never poll. `go build ./...` works without building the frontend.
