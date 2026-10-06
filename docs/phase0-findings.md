# Phase 0 findings

**Verdict: pass.** All Phase 0 exit criteria are met in both runs. Agents read each other's notes and changed what they did because of them. The Go task builds, vets and passes its tests. There was no over-posting. The weak spots are waiting (sleep loops, file polling), races in "claim then check" protocols, and agents running with the user's personal Claude Code plugins loaded.

Runs (2026-10-06, `claude-opus-5-5`, Claude Code 2.1.280):
- **smoke**: 3 sub-agents, each writes a two-line poem file (`a.txt`, `b.txt`, `c.txt`), and the themes must be distinct.
- **exp**: a Go todo CLI split into `internal/store`, `internal/format`, `internal/cli` + `main.go`. The shared types are agreed through notes.

## Exit criteria

| Criterion | Result | Evidence |
|---|---|---|
| Single binary builds and runs | ✅ | `go build -o fragile ./cmd/fragile`. stdout: `fragile notes server listening on http://127.0.0.1:7777 (session 1)` |
| ≥3 sub-agents, separate OS processes | ✅ | smoke pids 14274 (orch), 14324, 14354, 14384. exp pids 16308 (orch), 16818, 16855, 16923. `select count(*), count(distinct pid) from agent_instances` gives `4\|4` in both runs. Each agent has its own `session_id` and its own `agents/agent-N.jsonl` |
| Notes visible across agents | ✅ | Every sub-agent's first call was `read_notes("session")`, and it got the orchestrator's note #1. smoke: agent 4 read #2 and reacted: *"Note #2 (b.txt) has same V-formation theme, lower id. Switch, avoid birds."* exp: CLI's done note says it ran the build *"after notes #2/#3"* (the other two agents' done notes) |
| Every note attributed correctly | ✅ | Each `post_note` result id in agent N's log was matched to `notes.author_agent_id`: 12/12 (smoke) and 4/4 (exp), 0 mismatches. When another agent resolved a note, the author was kept: `note_updated agent_id=1 … author_agent_id=4` (note #4) |
| All activity in SQLite and events.jsonl | ✅ | smoke: 4 `agent_spawned` / 4 agents. 12 `note_posted` / 12 notes (content and author identical). 5 `note_updated` / 5 `update_note` calls. 4 `agent_status_changed`. exp: 4/4, 4/4, 0/0, 4. 0 escalations (`escalate_to_user` was never needed) |
| Built-in sub-agent tool disallowed | ✅ | `--disallowedTools Task,Agent,Workflow` (`notes/runner.go`). In all 8 `system/init` events, `tools` contains none of Task/Agent/Workflow. Sub-agents see only `read_notes`, `post_note` and `update_note` from fragile. No agent attempted the tools (0 tool_use calls) |
| exp build | ✅ | `go build ./... && go vet ./... && go test -count=1 ./...`: `ok internal/cli`, `ok internal/format`, `ok internal/store`, exit 0 (root package has no tests) |

## Per-run facts

| | smoke | exp |
|---|---|---|
| Agents | 1 orch + 3 sub | 1 orch + 3 sub |
| Notes (total) | 12 | 4 |
| decision / heads_up / done / question / blocker | 3 / 6 / 3 / 0 / 0 | 1 / 0 / 3 / 0 / 0 |
| Notes per agent (orch, a2, a3, a4) | 3, 4, 2, 3 | 1, 1, 1, 1 |
| Wall clock (launch to orch exit) | 94 s | 106 s |
| Orch to first spawn | 11 s | 24 s (wrote a 1.7 KB contract note first) |
| Sub-agent durations | 65 / 42 / 55 s | 34 / 29 / 50 s |
| num_turns (orch, subs) | 20; 15 / 12 / 14 | 15; 8 / 8 / 11 |
| Cost (`total_cost_usd`, sum) | $1.33 (orch $0.52, subs $0.26 to $0.28) | $1.24 (orch $0.39, subs $0.24 to $0.35) |
| Exit codes | all 0 | all 0 |

Each sub-agent costs about $0.25 even for a two-line poem. Most of that is the roughly 20k-token system prompt cache write per process, and it is inflated by the user's plugins (see confusion #3).

## Findings

### Useful: notes changed behaviour
- **smoke:** the board resolved theme clashes with no help from a human. Timeline: three `heads_up` claims (+21 s). Agent 4 sees it duplicates #2 and switches (#5). Agent 2 drops "starlings" as too close to #2 (#6). The orchestrator settles the remaining clash (#8, +49 s). All three done notes cite the notes they followed, e.g. `"(heads_up #5, confirmed by decision #8)"`.
- **smoke:** an agent cross-checked a peer. Agent 3's done note: *"Caveat … #6 (a.txt) now picks rowing crew, the same theme as #5 … a.txt should switch again."*
- **exp:** the single contract `decision` (#1) was enough. All three packages matched it on the first try, with no questions, blockers or rework. Done notes are high quality: files, API, how it was verified, caveats (*"`done` on already-done todo succeeds silently (spec silent)"*). The orchestrator copied these caveats straight into its summary.

### Over-posting: no
- No progress chatter in either run. Every note had a purpose.
- The 12 smoke notes come from the orchestrator's own protocol ("post heads_up, read, switch on clash"). Half are "theme CHANGED, supersedes #N" re-posts.
- exp is closer to under-use. There were 0 `heads_up` notes even though FORMAT and CLI depend on STORE. The orchestrator also pasted the whole contract into each task text (1.3 to 2.4 KB tasks), so note #1 duplicated it and the board carried almost no information. The exp task did not really stress note-based coordination.

### Ignoring others' notes: no, with one stale read
- Every agent read the board before starting and again before posting done. Agents used `since_id` correctly.
- Stale read: agent 3 (smoke) cites *"kept per decision #7"* after #7 had been superseded by #8 and resolved. It was harmless here. `read_notes` returns resolved notes by default, so "superseded" is easy to miss.

### Confusion
1. **Wrong-directory writes (smoke, agents 3 and 4).** Agent 4 wrote `c.txt` to `/private/tmp/claude-501/-Users-chhunly-Coding-projects-fragile-6d619670-…-scratchpad-smoke-work/c.txt`, then noticed (*"Wrote to wrong path (mangled dir name)"*) and moved the file into place. Agent 3's first `Write` used a similar hybrid path and failed with `EACCES … mkdir '/private/-Users-chhunly-Coding-projects-fragile'`.
   - **Cause:** each agent's `system/init` gives a `scratchpad_path` of `/private/tmp/claude-501/-private-tmp-claude-501--Users-chhunly-…-smoke-work/<session>/scratchpad`, which is a dash-mangled copy of the cwd. Because the working dir already sits inside a Claude scratchpad, the model blended the two long, near-identical paths. This came from the test setup (deep temp cwd), not from Fragile.
   - **Leftover:** an empty directory `/private/tmp/claude-501/-Users-…-smoke-work/` is still there. The orchestrator said it checked for strays, but it only ran `ls ..`.
2. **Race in claim-then-check (smoke).** Agents 2 and 4 switched to "rowing crew" 2.3 s apart (#5 at :48, #6 at :51). Neither saw the other's switch. The orchestrator's #7 (:52) was already stale (*"Agents 2 and 3 keep their themes"*, but agent 2 had switched), and #8 had to supersede it 6 s later. Polling with `sleep 10` plus separate post and read calls cannot prevent this.
3. **Agents inherit the user's Claude Code setup.** Sub-agent init shows `plugins ['agent-team','caveman','ponytail','agents-md','telemetry']` and 47 skills. SessionStart hooks inject *"PONYTAIL MODE ACTIVE"* and *"CAVEMAN MODE ACTIVE"*, which explains the terse internal text (*"Bad path. Use absolute."*). This muddies the experiment, adds tokens, and puts unrelated skills (e.g. `agent-team:team-lead`) in front of every agent.
4. **Waiting is improvised and bypasses the board.**
   - smoke: the orchestrator used `sleep 20` as the prompt says, which works.
   - exp: the orchestrator tried `sleep 60` and the harness blocked it (*"Blocked: standalone sleep 60. To wait for a condition, use Monitor with an until-loop"*). It then polled the filesystem (`until [ -f internal/store/store_test.go ] …`) and the process table (`until ! ps -p 16923`). Neither uses `get_subagent_status` or notes.
   - exp: the orchestrator told FORMAT and CLI to *"sleep 15 in bash and re-check"* for the dependency files instead of waiting for STORE's `done` note. FORMAT ran `for i in $(seq 20); do ls internal/store/*.go … sleep 15; done`.
   - It worked because STORE was quick. With a slower dependency this means minutes of blind polling, and a half-written `store.go` could be picked up.
5. **Duplicated verification (minor).** The CLI agent and then the orchestrator both built and smoke-tested the binary. That costs about $0.05 and 10 s, but it is worth noting because the prompt says the orchestrator should not implement.
6. **One turn per agent spent on `ToolSearch`.** The fragile MCP tools are deferred, so every agent first calls `ToolSearch select:mcp__fragile__…` before it can read notes.

## Suggested changes (ranked)

1. **Add a blocking wait tool, e.g. `wait_for_notes(scope, since_id, timeout_s, types?)`** (and optionally `wait_for_subagents(timeout_s)` for the orchestrator). It returns as soon as a new note or status change arrives.
   - Removes all sleep loops, works around the harness `sleep` block, and keeps waiting on the board instead of `ps`/`ls`.
   - Update `notes/prompts/orchestrator.md` Workflow step 5 ("run `sleep 20`") and the sub-agent prompt to use it.
2. **Isolate agent config.** Launch agents without the user's plugins, hooks and skills (e.g. restrict `--setting-sources` to project, or a dedicated settings file / config dir). Then experiment results reflect Fragile's prompts only, and per-agent cost drops. Change this in `notes/runner.go` `args()`.
3. **State the working directory explicitly.** Add a `{{WORKDIR}}` line to `notes/prompts/subagent.md` and `orchestrator.md`: "All files you create live under `{{WORKDIR}}`. Use paths relative to it. Your scratchpad is for temp files only." Also run future experiments in a short path (e.g. `/tmp/fragile-exp`).
4. **Dependencies via `done` notes, not files.** In `orchestrator.md` "Write self-contained tasks", add: "For a dependency on another sub-agent, tell the agent to wait for that agent's `done` note (`wait_for_notes`), never to poll for files." Also: "Put shared contracts in a `decision` note and reference it by id in the task; do not paste them into every task." That keeps the board as the single source of truth.
5. **Cut claim races.**
   - `orchestrator.md`: "If you can assign distinct choices up front (names, themes, ports), do it in the `decision` note instead of having agents claim and negotiate."
   - Tool side: have `post_note` return notes posted since the caller's last read. Agent 2 would then have seen #5 the moment it posted #6.
6. **Make supersession explicit.** Add an optional `supersedes` id to `post_note` that auto-resolves the old note, and have `read_notes` default to `status=open`, or mark resolved notes clearly. This prevents the stale "#7" citation and halves the "theme CHANGED" re-posts.
7. **Orchestrator verification scope.** `orchestrator.md` "Finish": "Run the project's build/test once to confirm. Do not repeat sub-agents' manual smoke tests unless a done note is missing or vague."
8. **Small items.**
   - Avoid the `ToolSearch` turn if Claude Code allows marking the fragile MCP tools as always-loaded.
   - In `subagent.md`, encourage a `heads_up` when an agent finishes a package others import (e.g. "STORE API ready at internal/store"), so dependents can start without polling.
   - Next experiment: pick a task with a real mid-task interface change, so `heads_up`, `question` and `blocker` actually get used (none were used in either run).
