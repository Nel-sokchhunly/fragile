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

- Each agent connects to `http://127.0.0.1:7777/mcp/<agent_id>`; the server attributes every note to that agent.
- All agents get `read_notes`, `post_note`, `update_note`. Only the orchestrator gets `spawn_subagent`, `get_subagent_status`, `escalate_to_user`.
- Sub-agents are separate `claude -p` processes started by `spawn_subagent`. Claude Code's built-in sub-agent tools (`Task`, `Agent`, `Workflow`) are disallowed for every agent.
- `escalate_to_user` prints a marked banner to stdout and logs it; there is no way to answer in Phase 0.
- Prompts live in [notes/prompts](notes/prompts).
