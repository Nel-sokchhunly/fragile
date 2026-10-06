# Fragile — Project Spec

**Fragile — agile, for agents**

*Draft v0.9 (source for spec generation)*

*Name origin: agent team → agile team → Fragile. The orchestrator works like a scrum master, the notes board like an agile board, and the user is the product owner.*

---

## 0. READ FIRST — How to use this document

This is a **source document for spec generation** (e.g. with PRD and issue-breakdown skills). It is not a PRD itself, and it is not to be implemented directly. It describes a multi-phase project; **specs and issues are generated for one phase at a time.**

> **CURRENT PHASE: Phase 1 — MVP desktop app**

**Rules for the agent generating specs:**

1. **Generate a PRD for the current phase only.** Everything outside it is background context, not scope.
2. **Use each phase's "Build" list as scope, its "Do not build" list as explicit out-of-scope, and its "Exit criteria" as acceptance criteria.**
3. **Never turn `[EXPERIMENT]` or `[ROADMAP]` items into PRD scope or issues.** Mention them, if at all, only as future considerations the design must not block.
4. **Respect the phase labels.** Every feature in this document is tagged:

   | Label | Meaning |
   |---|---|
   | `[P0]` | In scope for Phase 0 |
   | `[P1]` | In scope for Phase 1 (MVP) |
   | `[P2]` | In scope for Phase 2 (v1) |
   | `[EXPERIMENT]` | Design idea only. **Not in scope** unless the user explicitly promotes it to a phase. |
   | `[ROADMAP]` | Future idea. **Not in scope.** |

5. **"Design for it" is not "build it."** Some later features need the data model to allow them (e.g. a `parent_id` field). Include the field in scope; do not include the feature.
6. **Decisions in this document are settled** (stack, sub-agent approach, phase boundaries). When something is ambiguous or missing, ask the user rather than inventing scope.
7. **Hard constraint for all phases:** sub-agents are never launched through Claude Code's built-in sub-agent feature (the Task/Agent tool). They are always launched by our own code via `spawn_subagent`. See section 5.4.

**Workflow:** when a phase's exit criteria are met, the user updates the CURRENT PHASE line above and generates the next phase's PRD.

---

## 1. Context — Problem

Working with AI coding agents today is mostly one-on-one: one human, one agent, one thread. When a task gets big, the human can tell the agent to spawn sub-agents, but:

- **Sub-agents are black boxes.** You only see what the main agent chooses to relay, usually a summary at the end.
- **You can't step in mid-task.** No way to watch or nudge a sub-agent that's going the wrong way.
- **Sub-agents can't talk to each other.** Everything routes through the main agent, so related work conflicts or gets duplicated.
- **Delegation is manual.** It only happens when the human remembers to ask for it.

Existing tools (e.g. cmux) solve the "many agents, one human" *attention* problem with sidebars and notifications, but leave agents isolated from one another. **Coordination is the missing layer.**

## 2. Context — Concept

A desktop app where each **session** runs as an **orchestra**: a main agent acting as orchestrator, spawning sub-agents that coordinate through a shared, scoped **notes board**. The human can see everything, drill into any agent, and only gets pulled in when a real decision is needed.

### Hierarchy

```
App
└── Sessions (isolated from each other)
    └── Orchestrator (one per session)
        └── Sub-agents (share the session's notes board)
```

- Sessions never share data with each other.
- Inside a session, the orchestrator and its sub-agents share a notes board.
- Each sub-agent has its own context window; the notes board acts as shared external memory.

### Conflict resolution and authority

1. Sub-agents coordinate through notes.
2. If notes conflict or agents disagree, **the orchestrator resolves it**.
3. If the orchestrator needs input, it **escalates to the user**.
4. The user always has final authority.

---

## 3. Phases

### Phase 0 — Notes server experiment `[P0]`

**Goal:** find out whether agents actually coordinate well through shared notes, before building any app UI.

**Build:**

- A **standalone Go binary** implementing the notes MCP server using the official MCP Go SDK, served over **local HTTP** (streamable HTTP transport).
- The Phase 0 tools from section 5.2: `read_notes`, `post_note`, `update_note`, `spawn_subagent`, `get_subagent_status`, `escalate_to_user`.
- **`spawn_subagent` launches a real headless Claude Code process** (streaming JSON output), configured to connect back to this same MCP server with its own agent identity. Each sub-agent's raw output stream is written to its own log file.
- **Agent identity:** every agent connects with its own ID (e.g. via URL path or header token) so the server knows who posted each note.
- **Storage:** SQLite, using the Phase 0 tables from section 5.3.
- **Human-readable observation log:** every note, spawn, status change, and escalation is also appended to a readable log (e.g. JSONL or Markdown) the user can watch live with `tail -f`.
- **`escalate_to_user` in Phase 0** just writes a clearly marked entry to the log and stdout. No UI.
- **A launcher command or script** that starts the server and starts an orchestrator Claude Code process connected to it, with:
  - Claude Code's built-in sub-agent tool **disallowed**.
  - An orchestrator system prompt explaining: break the task down, spawn sub-agents with `spawn_subagent`, coordinate through notes, escalate when needed.
- A sub-agent system prompt explaining: read notes before starting, post `heads_up` / `decision` / `blocker` / `question` notes as appropriate, post a `done` note with a summary when finished.

**Do not build:** any desktop app, any UI, Wails, React, multiple sessions, shared/private scopes (session-wide scope only), process restart/kill controls, nested sub-agents.

**Exit criteria:**

- [ ] Server builds and runs as a single binary.
- [ ] Orchestrator connects and can spawn at least 3 sub-agents, each as a **separate OS process**.
- [ ] Sub-agents read and post notes; notes from one agent are visible to the others.
- [ ] Every note is attributed to the correct agent.
- [ ] All activity is persisted to SQLite and appears in the readable log.
- [ ] Built-in sub-agent tool is confirmed disallowed.
- [ ] A short **findings report** for the user covering: did agents use notes usefully? Over-posting? Ignoring others' notes? Confusion? Suggested prompt or tool changes.

---

### Phase 1 — MVP desktop app `[P1]`

**Goal:** validate that an orchestrator + sub-agents coordinating through shared notes beats managing sub-agents by hand, with full visibility.

**Build:**

- Wails desktop app (Go backend + React frontend) per section 5.5.
- **Embed the Phase 0 notes server** in the Go backend (reuse, don't rewrite).
- **Sidebar** listing multiple sessions, each with a simple status: `working` / `done` / `needs you`. Create new sessions from the sidebar.
- **Every session runs in orchestra mode.** There is no normal mode yet.
- **Chat with the orchestrator** in each session (this is how the user gives the session its task and answers escalations).
- **Live notes board** per session (session-wide scope only), updating in real time.
- **Agent list** in each session using a **standalone agent card component** (name/task, status, latest note or output line).
- **Click into any sub-agent** to view its full output stream (view only).
- **Basic escalation:** `escalate_to_user` sets the session's `needs you` badge; the escalation appears in the orchestrator chat for the user to answer.
- **Persistence:** sessions, notes, tasks, and agent history saved in SQLite; past sessions can be reopened and viewed. Resuming live agent processes after an app restart is **not** required.
- **Scale-ready data model** (fields only, no features): `parent_id` on agent instances, a `tasks` table with status, generic scoped boards. See section 5.3.

**Do not build:** normal/orchestra toggle, escalation thresholds, escalation inbox, shared/private scopes, messaging sub-agents directly, Kanban, swarm features, session groups, other agent backends.

**Exit criteria:**

- [ ] User can create several sessions and run them in parallel.
- [ ] In each session, the user can give the orchestrator a task and watch it spawn sub-agents.
- [ ] Notes board, agent cards, and statuses update live without refreshing.
- [ ] Clicking a sub-agent shows its streaming output.
- [ ] Escalations raise the `needs you` badge and can be answered.
- [ ] Long agent logs stay smooth (virtualized).
- [ ] Closing and reopening the app shows past sessions and their history.

---

### Phase 2 — v1 `[P2]`

**Goal:** give the user finer control once the MVP has proven the idea.

**Build:**

- **Normal / orchestra toggle** per session: start as a normal one-on-one chat, flip to orchestra mode without starting over.
- **Escalation thresholds:** the user sets when the orchestrator should ask (e.g. "anything architectural", "only when blocked").
- **Escalation inbox:** escalations collected in one place instead of only inline in chat.
- **Shared and private note scopes** in the UI (data model already supports them).
- **Message a sub-agent directly** mid-task from its agent view.
- **Kill / restart** an individual sub-agent from the UI.

**Do not build:** Kanban, swarm features, session groups (all remain `[EXPERIMENT]` or `[ROADMAP]` unless promoted by the user).

**Exit criteria:** defined with the user when Phase 2 begins.

---

## 4. Out of scope — Experiments and Roadmap

**Nothing in this section is to be built** unless the user explicitly promotes it into a phase. It is here so current work does not block it.

### 4.1 Kanban view for sub-agents `[EXPERIMENT]`

- Cards are **tasks** with an agent attached. Columns: Planned → Working → Blocked → Review → Done.
- Cards move automatically from notes and process state (`blocker` note → Blocked, `done` note → Review, crashed process → flagged).
- **Planned column as an approval gate:** the orchestrator proposes tasks; the user reorders, edits, or drags a card to Working to approve spawning.
- Card contents: title, status dot, latest note/output line, elapsed time, unread/blocker badge. Click opens the agent view.
- Would introduce: a `create_task` tool (and `spawn_subagent` taking a task ID), a Review stage, and an authority rule (user actions win; orchestrator is notified).
- Open: should drags other than Planned → Working act as commands?

### 4.2 Swarm mode `[EXPERIMENT]`

Support sessions with hundreds of parallel sub-agents (real users already run 250+).

| Area | Phases 0–2 | Swarm mode |
|---|---|---|
| Hierarchy | One level deep | Nested: orchestrator → team leads → sub-agents |
| Notes | Session board | Strict scoping per team; leads post rollups upward |
| Escalation | Badge / inbox | Thresholds + batching; grouped escalations |
| UI | Individual agent cards | Aggregated counts, swimlanes, filters, exceptions-first view |
| Resources | One process per agent | Process pool with concurrency limit and queueing; optional remote agents |
| Limits | Not a concern | API rate limits and cost tracking |

Guiding principle: at swarm scale the question becomes "which few agents need attention?"

### 4.3 Roadmap `[ROADMAP]`

- **Session groups:** a shared board across sessions, while each keeps its internal board private.
- **Additional agent backends** (Codex, OpenCode, etc.).
- **Sidebar hints** like "Orchestra · 4 agents · 1 needs you".
- **Nested sub-agents** outside of swarm mode.

---

## 5. Reference design

Use this section as the technical reference for whichever phase is current. Phase tags show when each piece is needed.

### 5.1 Notes system

**Scopes** — boards are generic: each board has a scope type, an owner, and members.

| Scope | Visible to | Phase |
|---|---|---|
| Session-wide | All agents in the session | `[P0]` `[P1]` |
| Shared | Specific agents working on related pieces | Data model `[P1]`, UI `[P2]` |
| Private | One agent (scratch notes) | Data model `[P1]`, UI `[P2]` |

The orchestrator and the user can see all scopes. The user can add or edit notes `[P1]`.

**Note types** `[P0]`:

| Type | Meaning |
|---|---|
| `decision` | Agreed on; others should follow |
| `blocker` | Agent is stuck |
| `heads_up` | A change others may depend on |
| `done` | Work finished, with a summary |
| `question` | Needs an answer from another agent or the orchestrator |

### 5.2 MCP tools

| Tool | Caller | Purpose | Phase |
|---|---|---|---|
| `read_notes(scope, filter?)` | All agents | Read notes in an accessible scope | `[P0]` |
| `post_note(scope, type, content)` | All agents | Post a note | `[P0]` |
| `update_note(id, content \| status)` | All agents | Edit or resolve a note | `[P0]` |
| `spawn_subagent(task, scopes)` | Orchestrator only | App launches a new sub-agent process; also creates a task record | `[P0]` |
| `get_subagent_status(id)` | Orchestrator only | Check a sub-agent's status | `[P0]` |
| `escalate_to_user(question, context)` | Orchestrator only | Raise a decision to the user | `[P0]` (log only), `[P1]` (UI) |
| `create_task(...)` | Orchestrator only | Propose tasks before spawning | `[EXPERIMENT]` (Kanban) |

Sub-agents must not have access to orchestrator-only tools.

### 5.3 Data model (suggested; refine as needed)

| Table | Key fields | Phase |
|---|---|---|
| `sessions` | id, title, status, created_at | `[P0]` (single implicit session), `[P1]` |
| `agent_instances` | id, session_id, **parent_id**, role (`orchestrator` / `subagent`), task_id, status, pid, log_path, created_at | `[P0]` |
| `tasks` | id, session_id, title, description, status (`planned` / `working` / `blocked` / `review` / `done`), agent_id | `[P0]` (created by `spawn_subagent`) |
| `boards` | id, session_id, scope_type (`session` / `shared` / `private`), owner_id | `[P0]` (session scope only) |
| `board_members` | board_id, agent_id | `[P1]` (table exists), used `[P2]` |
| `notes` | id, board_id, author_agent_id, type, content, status, created_at, updated_at | `[P0]` |
| `escalations` | id, session_id, agent_id, question, context, status, answer | `[P0]` |
| `agent_events` | id, agent_id, event_type, payload, created_at | `[P1]` (for UI history) |

`parent_id` exists so nesting can be added later. **Do not implement nesting.**

### 5.4 Sub-agents

**Decision: sub-agents are launched by our own code through `spawn_subagent`, never by Claude Code's built-in sub-agent feature.** Built-in sub-agents run hidden inside the orchestrator's process, which recreates the black-box problem.

| | Built-in sub-agents | Our `spawn_subagent` |
|---|---|---|
| Where it runs | Inside the orchestrator's process | Its own headless process |
| Visibility | Partial | Full |
| Lifecycle | Tied to parent | Independent |
| Note access | Inherited | Scopes set per sub-agent |
| Agent-agnostic | No | Yes |

**Rules (all phases):**

1. Built-in sub-agent tool is **disallowed** for the orchestrator.
2. **One level deep.** Only the orchestrator spawns sub-agents.
3. **Sub-agents report back through notes**, posting a `done` note with a summary. The board is the single source of truth.
4. In code, the orchestrator and sub-agents are the same abstraction, an **agent instance**. Sub-agents belong to their session and never appear as separate sessions in the sidebar.

### 5.5 Tech stack

| Layer | Choice | Phase |
|---|---|---|
| Backend language | Go | `[P0]` |
| Notes server | Official MCP Go SDK, local HTTP | `[P0]` |
| Storage | SQLite | `[P0]` |
| Agent runtime | Claude Code headless mode as subprocesses, parsing streaming JSON | `[P0]` |
| Desktop shell | Wails (Go backend, web frontend) | `[P1]` |
| UI | React + TypeScript | `[P1]` |

**Why:** the app is a supervisor of many concurrent agent processes (goroutines fit), agents are driven as subprocesses so the design stays agent-agnostic, and the Phase 0 server is embedded in Phase 1 unchanged.

### 5.6 UI libraries `[P1]`

| Need | Library |
|---|---|
| Base components | shadcn/ui (Radix + Tailwind) |
| Resizable layout | react-resizable-panels (via shadcn Resizable) |
| Agent chat and logs | react-virtuoso (**virtualization is required**) |
| Markdown rendering | react-markdown |
| Code highlighting | Shiki |
| State management | Zustand |
| Icons | lucide-react |
| Drag and drop | dnd-kit — only if a promoted feature needs it |

**Event flow:** Go backend → Wails events → Zustand store → components. Components never poll.

### 5.7 Runtime flow

1. App (or Phase 0 binary) starts and hosts the notes MCP server on a local HTTP port.
2. Starting a session launches the **orchestrator** as a headless Claude Code process, connected to the server with its own identity, orchestrator tools, and the built-in sub-agent tool disallowed.
3. When the orchestrator calls `spawn_subagent`, our code creates a task record and launches a new headless Claude Code process for the sub-agent, connected with its own identity and scopes.
4. Every note and agent event goes through the Go backend, is persisted to SQLite, and (from Phase 1) is pushed to the UI.
5. `escalate_to_user` writes to the log (Phase 0) or sets the `needs you` badge and shows in chat (Phase 1).

---

## 6. Open questions

| Question | Blocks |
|---|---|
| UI layout details (worked out screen by screen with the user) | Phase 1 |
| How scopes are assigned at spawn time (orchestrator decides vs. defaults) | Phase 2 |
| Review stage and drag-as-command rules | Kanban experiment |

## 7. Known limitations

Found while building a phase and accepted for now. **Not scope** for the current phase unless the user promotes one; a phase's design must not make them worse. Each is tracked as a GitHub issue labelled `known-limitation`.

| Limitation | Found in | Impact | Fix direction | Tracking |
|---|---|---|---|---|
| Agents are only partly isolated from each other. Every agent runs in Claude Code's Bash sandbox (#40): writes only under its working directory and package caches, common credential files and token env vars hidden, network only to package registries and GitHub, and the agent dir (token files), DB and event log are denied to Bash and to Read/Edit/Write, so a sub-agent cannot read another agent's token. Still open: all agents run as the same OS user; the sandbox does not restrict Bash *reads* of other files outside the working directory (other projects); `WebFetch`/`WebSearch` and processes Claude Code launches itself (MCP servers, hooks) are not sandboxed; allowed hosts such as github.com can carry data out; Linux needs `bubblewrap` and `socat`. | Phase 0 review | Role gating and the one-level-deep rule (section 5.4) now hold against a sub-agent reading token files, not against a determined agent using the remaining gaps. | Separate OS user or container per agent; `denyRead` for the home directory; deliver the token without a readable file. | #37, #40 |
