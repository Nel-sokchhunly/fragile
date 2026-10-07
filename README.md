# Fragile

Agile, for agents. A desktop app where an orchestrator Claude Code agent splits your task across sub-agents that coordinate through a shared notes board. You are the product owner.

![Fragile: sessions, orchestrator chat, agents and notes](docs/screenshots/main.png)

## What it does

- **Sessions.** Create one from a folder, with an optional name (default: the folder name). The orchestrator starts on your first message. Sessions run in parallel; reopen past ones (history is in SQLite) or delete them. Status per session: `working`, `done`, `needs you`.
- **Orchestrator chat.** You give the task here. Its text, tool calls and escalations stream in live.
- **Agent cards.** One per sub-agent: task, live status and elapsed time, context usage (`34.5k/1M 3%`), latest output line. Click one for its full output stream (virtualized, view only).
- **Notes board.** The board the agents coordinate through (`decision`, `blocker`, `heads_up`, `done`, `question`). You can add notes and resolve or reopen them.
- **Escalations.** When the orchestrator calls `escalate_to_user`, the session turns `needs you` and the question shows in chat; answer it inline.
- **Subscription limits.** `5h 27% ↻4h28m · 7d 4% ↻6d` at the bottom of the sidebar, from what `claude` reports.
- **Stop** a session (kills the orchestrator and all its sub-agents).
- **Keyboard-first.** See [shortcuts](#keyboard-shortcuts).

## Screenshots

| | |
|---|---|
| ![An escalation waiting for an answer](docs/screenshots/needs-you.png) | ![A sub-agent's output stream](docs/screenshots/agent-output.png) |
| **Needs you:** the orchestrator escalates a naming decision; two sub-agents keep going. | **Agent output:** a sub-agent's full stream, tool calls collapsed. |
| ![New session dialog over the collapsed sidebar](docs/screenshots/new-session.png) | |
| **New session** (`⌘N`), with the sidebar collapsed to its rail (`⌘B`). | |

All screenshots are of a real run on a small Go TODO CLI.

## Install

Download the latest `build-N` release from [Releases](https://github.com/Nel-sokchhunly/fragile/releases). CI builds one on every push to `main`.

- **macOS** (`fragile-macos-universal.zip`): the app is unsigned. After unzipping, run once:
  ```bash
  xattr -dr com.apple.quarantine Fragile.app
  ```
- **Linux** (`fragile-linux-amd64.tar.gz`): needs `libgtk-3` and `libwebkit2gtk-4.1`. Agents need `bubblewrap` and `socat` (`apt install bubblewrap socat`); without them a session refuses to start.

Requires the [`claude` CLI](https://docs.claude.com/en/docs/claude-code) on `PATH`, logged in. Agents run on your Claude subscription; no API key needed.

## Build from source

Needs Go, Node, pnpm and the Wails CLI v2.14.0:

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@v2.14.0
cd app
wails dev     # live-reload window
wails build   # app/build/bin/Fragile.app on macOS
```

On Linux, add `-tags webkit2_41` (`wails build -tags webkit2_41`). `go build ./...` and `go test ./...` work without building the frontend.

Stack: Wails v2 (Go) + React, TypeScript, Tailwind/shadcn, Zustand, react-virtuoso. Backend state reaches the UI only as Wails events into Zustand stores; components never poll.

## Keyboard shortcuts

`⌘` is `Ctrl` on Linux.

| Keys | Action |
|---|---|
| `⌘B` | Collapse / expand the sidebar |
| `⌘K` or `/` | Focus the message box |
| `⌘N` | New session |
| `⌘1`–`⌘9` | Switch to session 1–9 |
| `Esc` | Agent output view → back to chat |
| `⌘⌫` | Delete the current session (asks first) |
| `Enter` / `Shift+Enter` | Send / new line (message box, escalation answer) |

## How it works

```
App (Go) ── notes MCP server, 127.0.0.1:<random port>, /mcp/<per-agent token>
 └─ session
     └─ orchestrator   claude -p, long-lived, chat over stdin (stream-json)
         └─ sub-agents claude -p, one process each, started by spawn_subagent
```

- **Notes server.** An MCP server (official Go SDK, streamable HTTP) on loopback only. Each agent gets its own random token in its URL, so every note is attributed to the right agent. Everything is stored in SQLite and appended to `events.jsonl`.
- **Tools per role.** All agents: `read_notes`, `post_note`, `update_note`, `wait_for_notes`. Orchestrator only: `spawn_subagent`, `get_subagent_status`, `escalate_to_user` (hidden from and refused for sub-agents). Sub-agents cannot spawn sub-agents (one level deep).
- **`wait_for_notes(since_id, timeout_s?, type?)`** blocks until a newer note exists (default 60 s, max 120 s), so agents never sleep or poll. The orchestrator can pass `finished_subagents` to also wake when a sub-agent exits. Wake-ups are in-process signals, not DB polling.
- **Agents are headless `claude -p` processes** with `--output-format stream-json`, each in its own process group. Claude Code's built-in sub-agent tools (`Task`, `Agent`, `Workflow`) are disallowed: every sub-agent is a visible process of its own.
- **Orchestrator runs like your own CLI.** No sandbox, your full Claude Code setup (user settings, plugins, hooks, skills, `CLAUDE.md`, memory) and `--permission-mode auto`: Claude Code's classifier runs routine actions and denies risky ones (headless, nothing can prompt), and the orchestrator escalates a denied action it needs to you right away, with the exact command so you can run it yourself. Only the fragile tools are pre-approved, so Bash still goes through the classifier. Your own deny rules apply.
- **Sub-agents are isolated from your Claude Code setup.** `--setting-sources project --disable-slash-commands` (no user plugins, hooks or skills; the repo's own `.claude` settings and `CLAUDE.md` still apply), `--strict-mcp-config`, and `CLAUDE_CODE_DISABLE_AUTO_MEMORY=1`. Your `claude` login still works.
- **Bash sandbox (sub-agents).** Every sub-agent runs with Claude Code's built-in sandbox (Seatbelt on macOS, bubblewrap + `socat` on Linux; no unsandboxed fallback). On Ubuntu 24.04+, AppArmor can block bubblewrap; allow it with an AppArmor profile for `bwrap` or `sudo sysctl kernel.apparmor_restrict_unprivileged_userns=0`. Bash can write only under the working directory and package caches (Go, npm, pnpm, cargo, the OS cache dir), cannot read common credentials (`~/.ssh`, `~/.aws`, `gh` and `claude` logins, `~/.netrc`, `~/.docker/config.json`, `~/.kube`) or token env vars, and can reach only package registries and GitHub. The agent dir (tokens, logs), the DB and the event log are hidden from Bash and from Read/Edit/Write, so a sub-agent cannot read the orchestrator's token. When the sandbox blocks something a task needs, the sub-agent posts a `blocker` note and the orchestrator does it outside the sandbox, or escalates to you if it is risky.
- **File tools (sub-agents).** `Edit`/`Write` are allowed only inside the session's working directory (anything else is denied, not prompted). `Read` is not limited to it, but the credential files above, the agent dir, the DB and the event log are denied to Read and Edit.
- **Subscription only.** Agents never receive `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_BASE_URL` or the Bedrock/Vertex switches, so they always run on your claude.ai login.
- **PATH.** On start the app merges your login shell's PATH, so it finds `claude`, node, pnpm and Homebrew tools even when opened from Finder or the Dock.
- **Lifecycle.** At most 8 running sub-agents per session; tasks up to 32 KB. Stopping a session marks agents `stopped`; agents left behind by a force-quit app are killed on the next start. Resume starts a new orchestrator that continues the previous Claude Code conversation (`claude --resume`) after a stop or an app restart; it is told its old sub-agents are gone and to pick up unfinished work.
- **Known limits.** All agents run as your OS user; the orchestrator is unsandboxed and relies on auto mode's classifier; Bash can still read other files outside the working directory; `WebFetch`/`WebSearch` and processes Claude Code starts itself (MCP servers, hooks) are not sandboxed; allowed hosts like github.com can carry data out. See [spec §7](fragile-spec.md#7-known-limitations).
- **Data** lives in `os.UserConfigDir()/Fragile` (`~/Library/Application Support/Fragile` on macOS, `~/.config/Fragile` on Linux): `fragile.db`, `events.jsonl`, `agents/agent-<id>.jsonl` (raw output) and `.mcp.json` configs. Override with `FRAGILE_DATA_DIR`. Deleting a session removes its rows and agent files, never your working directory.
- Prompts: [notes/prompts](notes/prompts).

## Headless CLI

`cmd/fragile` is the Phase 0 experiment runner: the same notes server and agents, no UI.

```bash
go build -o fragile ./cmd/fragile
./fragile -dir /path/to/repo "the task for the orchestrator"
tail -f .fragile/events.jsonl   # watch notes, spawns, escalations
```

It serves notes on `127.0.0.1:7777`, runs the orchestrator in `-dir` and exits when it and all sub-agents are done (Ctrl-C stops everything). Without a task it only serves notes. Escalations are printed, not answerable. Flags: `-addr`, `-dir`, `-db`, `-log`, `-agent-dir` (`./fragile -h`).

## Status

- **Phase 0** (notes server experiment): done. Findings in [docs/phase0-findings.md](docs/phase0-findings.md).
- **Phase 1** (MVP desktop app): done.
- **Phase 2** (v1): next: normal/orchestra toggle, escalation thresholds and inbox, shared/private note scopes, messaging and killing individual sub-agents. See [fragile-spec.md](fragile-spec.md).
- Known limitations: [spec §7](fragile-spec.md#7-known-limitations).
