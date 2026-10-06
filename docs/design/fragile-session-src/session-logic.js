
const ICON_DATA = {"plus":"data:image/svg+xml;base64,PCEtLSBAbGljZW5zZSBsdWNpZGUtc3RhdGljIHYwLjQ2MC4wIC0gSVNDIC0tPiA8c3ZnIGNsYXNzPSJsdWNpZGUgbHVjaWRlLXBsdXMiIHhtbG5zPSJodHRwOi8vd3d3LnczLm9yZy8yMDAwL3N2ZyIgd2lkdGg9IjI0IiBoZWlnaHQ9IjI0IiB2aWV3Qm94PSIwIDAgMjQgMjQiIGZpbGw9Im5vbmUiIHN0cm9rZT0iY3VycmVudENvbG9yIiBzdHJva2Utd2lkdGg9IjIiIHN0cm9rZS1saW5lY2FwPSJyb3VuZCIgc3Ryb2tlLWxpbmVqb2luPSJyb3VuZCIgPiA8cGF0aCBkPSJNNSAxMmgxNCIgLz4gPHBhdGggZD0iTTEyIDV2MTQiIC8+IDwvc3ZnPiA=","wrench":"data:image/svg+xml;base64,PCEtLSBAbGljZW5zZSBsdWNpZGUtc3RhdGljIHYwLjQ2MC4wIC0gSVNDIC0tPiA8c3ZnIGNsYXNzPSJsdWNpZGUgbHVjaWRlLXdyZW5jaCIgeG1sbnM9Imh0dHA6Ly93d3cudzMub3JnLzIwMDAvc3ZnIiB3aWR0aD0iMjQiIGhlaWdodD0iMjQiIHZpZXdCb3g9IjAgMCAyNCAyNCIgZmlsbD0ibm9uZSIgc3Ryb2tlPSJjdXJyZW50Q29sb3IiIHN0cm9rZS13aWR0aD0iMiIgc3Ryb2tlLWxpbmVjYXA9InJvdW5kIiBzdHJva2UtbGluZWpvaW49InJvdW5kIiA+IDxwYXRoIGQ9Ik0xNC43IDYuM2ExIDEgMCAwIDAgMCAxLjRsMS42IDEuNmExIDEgMCAwIDAgMS40IDBsMy43Ny0zLjc3YTYgNiAwIDAgMS03Ljk0IDcuOTRsLTYuOTEgNi45MWEyLjEyIDIuMTIgMCAwIDEtMy0zbDYuOTEtNi45MWE2IDYgMCAwIDEgNy45NC03Ljk0bC0zLjc2IDMuNzZ6IiAvPiA8L3N2Zz4g","arrow-left":"data:image/svg+xml;base64,PCEtLSBAbGljZW5zZSBsdWNpZGUtc3RhdGljIHYwLjQ2MC4wIC0gSVNDIC0tPiA8c3ZnIGNsYXNzPSJsdWNpZGUgbHVjaWRlLWFycm93LWxlZnQiIHhtbG5zPSJodHRwOi8vd3d3LnczLm9yZy8yMDAwL3N2ZyIgd2lkdGg9IjI0IiBoZWlnaHQ9IjI0IiB2aWV3Qm94PSIwIDAgMjQgMjQiIGZpbGw9Im5vbmUiIHN0cm9rZT0iY3VycmVudENvbG9yIiBzdHJva2Utd2lkdGg9IjIiIHN0cm9rZS1saW5lY2FwPSJyb3VuZCIgc3Ryb2tlLWxpbmVqb2luPSJyb3VuZCIgPiA8cGF0aCBkPSJtMTIgMTktNy03IDctNyIgLz4gPHBhdGggZD0iTTE5IDEySDUiIC8+IDwvc3ZnPiA=","terminal":"data:image/svg+xml;base64,PCEtLSBAbGljZW5zZSBsdWNpZGUtc3RhdGljIHYwLjQ2MC4wIC0gSVNDIC0tPiA8c3ZnIGNsYXNzPSJsdWNpZGUgbHVjaWRlLXRlcm1pbmFsIiB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHdpZHRoPSIyNCIgaGVpZ2h0PSIyNCIgdmlld0JveD0iMCAwIDI0IDI0IiBmaWxsPSJub25lIiBzdHJva2U9ImN1cnJlbnRDb2xvciIgc3Ryb2tlLXdpZHRoPSIyIiBzdHJva2UtbGluZWNhcD0icm91bmQiIHN0cm9rZS1saW5lam9pbj0icm91bmQiID4gPHBvbHlsaW5lIHBvaW50cz0iNCAxNyAxMCAxMSA0IDUiIC8+IDxsaW5lIHgxPSIxMiIgeDI9IjIwIiB5MT0iMTkiIHkyPSIxOSIgLz4gPC9zdmc+IA==","message-square":"data:image/svg+xml;base64,PCEtLSBAbGljZW5zZSBsdWNpZGUtc3RhdGljIHYwLjQ2MC4wIC0gSVNDIC0tPiA8c3ZnIGNsYXNzPSJsdWNpZGUgbHVjaWRlLW1lc3NhZ2Utc3F1YXJlIiB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHdpZHRoPSIyNCIgaGVpZ2h0PSIyNCIgdmlld0JveD0iMCAwIDI0IDI0IiBmaWxsPSJub25lIiBzdHJva2U9ImN1cnJlbnRDb2xvciIgc3Ryb2tlLXdpZHRoPSIyIiBzdHJva2UtbGluZWNhcD0icm91bmQiIHN0cm9rZS1saW5lam9pbj0icm91bmQiID4gPHBhdGggZD0iTTIxIDE1YTIgMiAwIDAgMS0yIDJIN2wtNCA0VjVhMiAyIDAgMCAxIDItMmgxNGEyIDIgMCAwIDEgMiAyeiIgLz4gPC9zdmc+IA==","gavel":"data:image/svg+xml;base64,PCEtLSBAbGljZW5zZSBsdWNpZGUtc3RhdGljIHYwLjQ2MC4wIC0gSVNDIC0tPiA8c3ZnIGNsYXNzPSJsdWNpZGUgbHVjaWRlLWdhdmVsIiB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHdpZHRoPSIyNCIgaGVpZ2h0PSIyNCIgdmlld0JveD0iMCAwIDI0IDI0IiBmaWxsPSJub25lIiBzdHJva2U9ImN1cnJlbnRDb2xvciIgc3Ryb2tlLXdpZHRoPSIyIiBzdHJva2UtbGluZWNhcD0icm91bmQiIHN0cm9rZS1saW5lam9pbj0icm91bmQiID4gPHBhdGggZD0ibTE0LjUgMTIuNS04IDhhMi4xMTkgMi4xMTkgMCAxIDEtMy0zbDgtOCIgLz4gPHBhdGggZD0ibTE2IDE2IDYtNiIgLz4gPHBhdGggZD0ibTggOCA2LTYiIC8+IDxwYXRoIGQ9Im05IDcgOCA4IiAvPiA8cGF0aCBkPSJtMjEgMTEtOC04IiAvPiA8L3N2Zz4g","circle-help":"data:image/svg+xml;base64,PCEtLSBAbGljZW5zZSBsdWNpZGUtc3RhdGljIHYwLjQ2MC4wIC0gSVNDIC0tPiA8c3ZnIGNsYXNzPSJsdWNpZGUgbHVjaWRlLWNpcmNsZS1oZWxwIiB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHdpZHRoPSIyNCIgaGVpZ2h0PSIyNCIgdmlld0JveD0iMCAwIDI0IDI0IiBmaWxsPSJub25lIiBzdHJva2U9ImN1cnJlbnRDb2xvciIgc3Ryb2tlLXdpZHRoPSIyIiBzdHJva2UtbGluZWNhcD0icm91bmQiIHN0cm9rZS1saW5lam9pbj0icm91bmQiID4gPGNpcmNsZSBjeD0iMTIiIGN5PSIxMiIgcj0iMTAiIC8+IDxwYXRoIGQ9Ik05LjA5IDlhMyAzIDAgMCAxIDUuODMgMWMwIDItMyAzLTMgMyIgLz4gPHBhdGggZD0iTTEyIDE3aC4wMSIgLz4gPC9zdmc+IA==","octagon-alert":"data:image/svg+xml;base64,PCEtLSBAbGljZW5zZSBsdWNpZGUtc3RhdGljIHYwLjQ2MC4wIC0gSVNDIC0tPiA8c3ZnIGNsYXNzPSJsdWNpZGUgbHVjaWRlLW9jdGFnb24tYWxlcnQiIHhtbG5zPSJodHRwOi8vd3d3LnczLm9yZy8yMDAwL3N2ZyIgd2lkdGg9IjI0IiBoZWlnaHQ9IjI0IiB2aWV3Qm94PSIwIDAgMjQgMjQiIGZpbGw9Im5vbmUiIHN0cm9rZT0iY3VycmVudENvbG9yIiBzdHJva2Utd2lkdGg9IjIiIHN0cm9rZS1saW5lY2FwPSJyb3VuZCIgc3Ryb2tlLWxpbmVqb2luPSJyb3VuZCIgPiA8cGF0aCBkPSJNMTIgMTZoLjAxIiAvPiA8cGF0aCBkPSJNMTIgOHY0IiAvPiA8cGF0aCBkPSJNMTUuMzEyIDJhMiAyIDAgMCAxIDEuNDE0LjU4Nmw0LjY4OCA0LjY4OEEyIDIgMCAwIDEgMjIgOC42ODh2Ni42MjRhMiAyIDAgMCAxLS41ODYgMS40MTRsLTQuNjg4IDQuNjg4YTIgMiAwIDAgMS0xLjQxNC41ODZIOC42ODhhMiAyIDAgMCAxLTEuNDE0LS41ODZsLTQuNjg4LTQuNjg4QTIgMiAwIDAgMSAyIDE1LjMxMlY4LjY4OGEyIDIgMCAwIDEgLjU4Ni0xLjQxNGw0LjY4OC00LjY4OEEyIDIgMCAwIDEgOC42ODggMnoiIC8+IDwvc3ZnPiA=","sticky-note":"data:image/svg+xml;base64,PCEtLSBAbGljZW5zZSBsdWNpZGUtc3RhdGljIHYwLjQ2MC4wIC0gSVNDIC0tPiA8c3ZnIGNsYXNzPSJsdWNpZGUgbHVjaWRlLXN0aWNreS1ub3RlIiB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHdpZHRoPSIyNCIgaGVpZ2h0PSIyNCIgdmlld0JveD0iMCAwIDI0IDI0IiBmaWxsPSJub25lIiBzdHJva2U9ImN1cnJlbnRDb2xvciIgc3Ryb2tlLXdpZHRoPSIyIiBzdHJva2UtbGluZWNhcD0icm91bmQiIHN0cm9rZS1saW5lam9pbj0icm91bmQiID4gPHBhdGggZD0iTTE2IDNINWEyIDIgMCAwIDAtMiAydjE0YTIgMiAwIDAgMCAyIDJoMTRhMiAyIDAgMCAwIDItMlY4WiIgLz4gPHBhdGggZD0iTTE1IDN2NGEyIDIgMCAwIDAgMiAyaDQiIC8+IDwvc3ZnPiA=","check":"data:image/svg+xml;base64,PCEtLSBAbGljZW5zZSBsdWNpZGUtc3RhdGljIHYwLjQ2MC4wIC0gSVNDIC0tPiA8c3ZnIGNsYXNzPSJsdWNpZGUgbHVjaWRlLWNoZWNrIiB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHdpZHRoPSIyNCIgaGVpZ2h0PSIyNCIgdmlld0JveD0iMCAwIDI0IDI0IiBmaWxsPSJub25lIiBzdHJva2U9ImN1cnJlbnRDb2xvciIgc3Ryb2tlLXdpZHRoPSIyIiBzdHJva2UtbGluZWNhcD0icm91bmQiIHN0cm9rZS1saW5lam9pbj0icm91bmQiID4gPHBhdGggZD0iTTIwIDYgOSAxN2wtNS01IiAvPiA8L3N2Zz4g"};
const ICON = n => ICON_DATA[n];
const fmt = s => { s = Math.max(0, Math.round(s)); const m = Math.floor(s / 60), r = s % 60; return m ? `${m}m${String(r).padStart(2, "0")}s` : `${r}s`; };
const NOTE_COLOR = { decision: "var(--status-decision)", question: "var(--status-attention)", blocker: "var(--status-blocked)", heads_up: "var(--text-secondary)", done: "var(--status-done)" };
const NOTE_ICON = { decision: "gavel", question: "circle-help", blocker: "octagon-alert", heads_up: "sticky-note", done: "check" };
const SESSION_COLOR = { working: "var(--status-running)", "needs you": "var(--status-attention)", done: "var(--text-muted)", new: "var(--text-muted)" };

const POOL = {
  "2": [["tool", "Edit storage/sqlite.go"], ["tool", "go test ./storage/..."], ["text", "Migration creates todos table with autoincrement id."]],
  "3": [["tool", "Edit cli/done.go"], ["tool", "Edit cli/rm.go"], ["text", "Wiring commands to Store interface."], ["tool", "go test ./cli/..."]],
};

function seed() {
  return [
    { id: "todo", title: "Todo CLI", created: 0,
      messages: [
        { k: "user", text: "Build a Go todo CLI" },
        { k: "assistant", text: "Three sub-agents, one per package: storage, cli, format. Shared types posted as a decision so they agree on the shape of a todo." },
        { k: "tool", text: "spawn_subagent x3" },
        { k: "assistant", text: "Format is done. Storage needs a call from you before it picks a backend." },
        { k: "esc", from: "#2 storage", text: "JSON file or SQLite for storage?", options: ["JSON file", "SQLite"], answer: null, agent: 2 },
      ],
      agents: [
        { id: 2, name: "storage package", task: "Store interface + file backend", status: "running", start: 0,
          stream: [{ t: 2, k: "text", text: "Reading notes before starting." }, { t: 9, k: "tool", text: "Edit storage/store.go" }, { t: 12, k: "note", type: "heads_up", text: "Store interface ready" }, { t: 70, k: "tool", text: "go test ./storage/..." }, { t: 95, k: "note", type: "question", text: "JSON or SQLite? asked orchestrator" }] },
        { id: 3, name: "cli package", task: "add, list, done, rm commands", status: "running", start: 5,
          stream: [{ t: 7, k: "text", text: "Reading notes before starting." }, { t: 30, k: "tool", text: "Edit cli/root.go" }, { t: 58, k: "tool", text: "Edit cli/add.go" }, { t: 101, k: "note", type: "question", text: "IDs start at 1?" }, { t: 118, k: "tool", text: "Edit cli/list.go" }] },
        { id: 4, name: "format package", task: "Aligned table output", status: "done", start: 5, end: 105,
          stream: [{ t: 8, k: "text", text: "Reading notes before starting." }, { t: 22, k: "tool", text: "Edit format/table.go" }, { t: 80, k: "tool", text: "go test ./format/..." }, { t: 100, k: "note", type: "done", text: "table printer + tests" }] },
      ],
      notes: [
        { type: "decision", author: "orchestrator", text: "Todo{ID,Text,Done}" },
        { type: "heads_up", author: "#2", text: "Store interface ready" },
        { type: "question", author: "#3", text: "IDs start at 1?" },
        { type: "done", author: "#4", text: "format finished" },
      ] },
    { id: "auth", title: "Auth refactor", created: -400,
      messages: [
        { k: "user", text: "Move auth to the new session middleware" },
        { k: "assistant", text: "Two sub-agents: one migrates handlers, one rewrites the token store." },
        { k: "tool", text: "spawn_subagent x2" },
        { k: "esc", from: "#3 token store", text: "Keep legacy cookies valid for 30 days?", options: ["Yes, 30 days", "No, invalidate now"], answer: null, agent: 3 },
      ],
      agents: [
        { id: 2, name: "handlers", task: "Swap handlers to session middleware", status: "running", start: -380, stream: [{ t: 10, k: "tool", text: "Edit http/login.go" }, { t: 200, k: "tool", text: "Edit http/logout.go" }] },
        { id: 3, name: "token store", task: "Redis-backed sessions", status: "running", start: -370, stream: [{ t: 40, k: "tool", text: "Edit auth/store.go" }, { t: 300, k: "note", type: "blocker", text: "cookie policy unclear" }] },
      ],
      notes: [{ type: "decision", author: "orchestrator", text: "Session TTL 7d" }, { type: "blocker", author: "#3", text: "cookie policy unclear" }] },
    { id: "docs", title: "Docs pass", created: -2000,
      messages: [
        { k: "user", text: "Tidy the README and add usage examples" },
        { k: "assistant", text: "Done. README rewritten with install, usage and a flag reference." },
      ],
      agents: [{ id: 2, name: "readme", task: "Install, usage, flags", status: "done", start: -1990, end: -1800, stream: [{ t: 20, k: "tool", text: "Edit README.md" }, { t: 190, k: "note", type: "done", text: "README rewritten" }] }],
      notes: [{ type: "done", author: "#2", text: "README merged" }] },
  ];
}

class Component extends DCLogic {
  state = { now: 140, sessions: seed(), cur: "todo", agent: null, draft: "", noteInputOpen: false, noteDraft: "" };

  componentDidMount() {
    this.timer = setInterval(() => {
      this.setState(s => {
        const now = s.now + 1;
        if (now % 4) return { now };
        const sessions = s.sessions.map(se => {
          if (se.id !== "todo") return se;
          return { ...se, agents: se.agents.map(a => {
            const pool = POOL[a.id];
            if (a.status !== "running" || !pool || Math.random() < 0.5) return a;
            const [k, text] = pool[Math.floor(Math.random() * pool.length)];
            return { ...a, stream: [...a.stream, { t: now - a.start, k, text }] };
          }) };
        });
        return { now, sessions };
      });
    }, 1000);
  }
  componentWillUnmount() { clearInterval(this.timer); }

  patch(id, fn) { this.setState(s => ({ sessions: s.sessions.map(se => (se.id === id ? fn(se, s) : se)) })); }

  answer(sid, text) {
    this.patch(sid, se => {
      const esc = se.messages.find(m => m.k === "esc" && !m.answer);
      return {
        ...se,
        messages: [...se.messages.map(m => (m === esc ? { ...m, answer: text } : m)), { k: "assistant", text: `Noted. Posted as a decision; ${esc.from.split(" ")[0]} continues with it.` }],
        notes: [{ type: "decision", author: "you", text }, ...se.notes],
      };
    });
  }

  send() {
    const text = this.state.draft.trim();
    if (!text) return;
    const sid = this.state.cur;
    const se = this.state.sessions.find(x => x.id === sid);
    this.setState({ draft: "" });
    if (se.messages.some(m => m.k === "esc" && !m.answer)) {
      this.patch(sid, x => ({ ...x, messages: [...x.messages, { k: "user", text }] }));
      setTimeout(() => this.answer(sid, text), 300);
      return;
    }
    const first = se.messages.length === 0;
    this.patch(sid, x => ({ ...x, title: first ? text.slice(0, 28) : x.title, messages: [...x.messages, { k: "user", text }] }));
    setTimeout(() => this.patch(sid, (x, s) => first
      ? { ...x, messages: [...x.messages, { k: "assistant", text: "Splitting this into two packages. Spawning sub-agents now." }, { k: "tool", text: "spawn_subagent x2" }],
          agents: [{ id: 2, name: "core package", task: "Domain types + logic", status: "running", start: s.now, stream: [{ t: 0, k: "text", text: "Reading notes before starting." }] },
                   { id: 3, name: "interface package", task: "Entry point + wiring", status: "running", start: s.now, stream: [{ t: 0, k: "text", text: "Reading notes before starting." }] }],
          notes: [{ type: "decision", author: "orchestrator", text: "Two packages: core, interface" }] }
      : { ...x, messages: [...x.messages, { k: "assistant", text: "Understood. Passing that to the running agents through the notes board." }] }), 700);
  }

  statusOf(se) {
    if (se.messages.some(m => m.k === "esc" && !m.answer)) return "needs you";
    if (!se.agents.length) return se.messages.length ? "working" : "new";
    return se.agents.some(a => a.status === "running") ? "working" : "done";
  }

  agentStatus(a) {
    const now = this.state.now;
    return a.status === "running"
      ? { label: `running · ${fmt(now - a.start)}`, style: { color: "var(--status-running)", whiteSpace: "nowrap", flex: "none" } }
      : { label: `done · ${fmt(a.end - a.start)}`, style: { color: "var(--text-muted)", whiteSpace: "nowrap", flex: "none" } };
  }

  renderVals() {
    const { sessions, cur, agent, draft, noteInputOpen, noteDraft } = this.state;
    const se = sessions.find(x => x.id === cur) || sessions[0];
    const st = this.statusOf(se);
    const running = se.agents.filter(a => a.status === "running").length;
    const done = se.agents.length - running;
    const openEsc = se.messages.some(m => m.k === "esc" && !m.answer);
    const mask = n => { const m = `url(${ICON(n)}) center/contain no-repeat`; return { width: 13, height: 13, flex: "none", background: "var(--text-muted)", WebkitMask: m, mask: m }; };

    const agents = se.agents.map(a => {
      const s = this.agentStatus(a);
      const last = a.stream[a.stream.length - 1] || { k: "text", text: "starting" };
      const icon = last.k === "note" ? NOTE_ICON[last.type] : last.k === "tool" ? "terminal" : "message-square";
      return { id: a.id, name: a.name, task: a.task, statusLabel: s.label, statusStyle: s.style,
        latest: last.k === "note" ? `${last.type}: ${last.text}` : last.text, iconStyle: mask(icon),
        open: () => this.setState({ agent: a.id }) };
    });

    const ag = se.agents.find(a => a.id === agent);
    const agS = ag ? this.agentStatus(ag) : { label: "", style: {} };

    return {
      sessions: sessions.map(x => {
        const s = this.statusOf(x);
        return { title: x.title, statusLabel: s,
          statusStyle: { color: SESSION_COLOR[s] },
          select: () => this.setState({ cur: x.id, agent: null, noteInputOpen: false }),
          rowStyle: { font: "inherit", textAlign: "left", display: "flex", flexDirection: "column", alignItems: "flex-start", width: "100%", padding: "6px 10px", borderRadius: 10, cursor: "pointer", color: "var(--text-primary)",
            background: x.id === cur ? "var(--surface-panel)" : "transparent", border: "1px solid " + (x.id === cur ? "var(--border-strong)" : "transparent") } };
      }),
      newSession: () => this.setState(s => {
        const id = "s" + Date.now();
        return { sessions: [{ id, title: "New session", messages: [], agents: [], notes: [] }, ...s.sessions], cur: id, agent: null };
      }),
      cur: {
        title: se.title, statusLabel: st, statusStyle: { color: SESSION_COLOR[st], whiteSpace: "nowrap" },
        agentSummary: se.agents.length ? `${running} running · ${done} done` : "no agents",
        empty: se.messages.length === 0,
        messages: se.messages.map(m => ({
          text: m.text, from: m.from, answer: m.answer,
          isUser: m.k === "user", isAssistant: m.k === "assistant", isTool: m.k === "tool",
          isOpenEsc: m.k === "esc" && !m.answer, isClosedEsc: m.k === "esc" && !!m.answer,
          options: (m.options || []).map(o => ({ label: o, pick: () => this.answer(se.id, o) })),
        })),
        agents, noAgents: agents.length === 0,
        notes: se.notes.map(n => ({ ...n, kindStyle: { color: NOTE_COLOR[n.type] } })),
      },
      placeholder: openEsc ? "Answer the escalation, or message the orchestrator" : se.messages.length ? "Message the orchestrator" : "Describe the task",
      draft,
      onDraft: e => this.setState({ draft: e.target.value }),
      onKey: e => { if (e.key === "Enter") this.send(); },
      listMode: !ag, agentMode: !!ag,
      closeAgent: () => this.setState({ agent: null }),
      ag: ag ? {
        id: ag.id, name: ag.name, task: ag.task, statusLabel: agS.label, statusStyle: agS.style, live: ag.status === "running",
        stream: ag.stream.map(l => ({
          time: fmt(l.t), text: l.text, tag: l.k === "note" ? l.type + " " : l.k === "tool" ? "› " : "",
          tagStyle: { color: l.k === "note" ? NOTE_COLOR[l.type] : "var(--text-muted)" },
          style: { color: l.k === "tool" ? "var(--text-secondary)" : "var(--text-primary)", fontFamily: l.k === "tool" ? "var(--font-mono)" : "inherit", fontSize: l.k === "tool" ? 12 : 14 },
        })),
      } : { stream: [] },
      noteInputOpen, noteDraft,
      toggleNoteInput: () => this.setState(s => ({ noteInputOpen: !s.noteInputOpen })),
      onNoteDraft: e => this.setState({ noteDraft: e.target.value }),
      onNoteKey: e => {
        if (e.key === "Escape") this.setState({ noteInputOpen: false });
        if (e.key !== "Enter" || !noteDraft.trim()) return;
        const text = noteDraft.trim();
        this.setState({ noteDraft: "", noteInputOpen: false });
        this.patch(se.id, x => ({ ...x, notes: [{ type: "decision", author: "you", text }, ...x.notes] }));
      },
    };
  }
}
