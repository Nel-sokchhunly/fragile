# Fragile session view: design spec

Source: `docs/design/fragile-session.html` (Claude Design export, "Session.dc.html"). Unpacked sources: `fragile-session-src/`. Screenshots: `fragile-session-shots/`.

| Shot | State |
|---|---|
| 01 / 02 | Default at 1440x900 / 1280x800 (Todo CLI session, open escalation) |
| 03 / 04 | Agent card hover / session row hover |
| 05 | Composer focused with text |
| 06 | Notes: inline "add note" input open |
| 07 / 08 | Agent view (right column) for a running agent / a done agent |
| 09 | Escalation answered (closed block, decision note added) |
| 10 / 11 | Session with a blocker note / finished session |
| 12 / 13 | New session, empty state (1440 / 1280) |
| 14 | 900px wide (column minimums) |

**Scope of the design.** Dark theme only: there are no light values and no theme toggle. It also omits a collapsed sidebar, dialogs, resizable panes, a selected agent card, crashed/exited agents, an orchestrator ("lead") card, note resolve, a stop button and timestamps. Sections below mark these "not in design".

---

## 1. Tokens

Every value is defined on `:root` (dark). In the "Light" column, "n/a" means the design gives no value. "Ours" names the existing token in `app/frontend/src/index.css` (`.dark`) it should map to.

### Palette (raw)
| Token | Value |
|---|---|
| `--neutral-950` | `#141413` |
| `--neutral-900` | `#1a1a19` |
| `--neutral-850` | `#1c1c1b` |
| `--neutral-800` | `#1f1f1e` |
| `--neutral-750` | `#262624` |
| `--neutral-700` | `#2f2f2d` |
| `--neutral-650` | `#3a3a37` |
| `--neutral-600` | `#55544f` |
| `--neutral-400` | `#8f8e89` |
| `--neutral-200` | `#c9c8c2` |
| `--neutral-50` | `#f2f1ec` |
| `--green-500` | `#34b233` |
| `--amber-500` | `#e39b0e` |
| `--amber-800` | `#6e4605` |
| `--amber-900` | `#3a2502` |
| `--blue-400` | `#6ea8ef` |
| `--red-400` | `#e5655a` |

The neutrals are a warm charcoal ramp: they are slightly yellow, where our shadcn ramp is pure grey (`oklch(x 0 0)`).

### Semantic
| Token | Dark | Light | Purpose | Ours (maps to) |
|---|---|---|---|---|
| `--surface-page` | `#1a1a19` | n/a | body bg (covered by the app grid) | `--background` |
| `--surface-frame` | `#1c1c1b` | n/a | sidebar bg | `--sidebar` |
| `--surface-panel` | `#1f1f1e` | n/a | main + right column bg; selected session row bg | `--background` / `--card` |
| `--surface-sunken` | `#141413` | n/a | user bubble, composer bg | none (closest `--muted`); **add** e.g. `--surface-sunken` |
| `--surface-hover` | `#262624` | n/a | hover bg (cards, icon buttons) | `--accent`, `--sidebar-accent` |
| `--surface-escalation` | `#3a2502` | n/a | open escalation bg (solid) | `--escalation` (ours is used at 10% alpha) |
| `--border-subtle` | `#2f2f2d` | n/a | column and header dividers | `--border`, `--sidebar-border` |
| `--border-default` | `#3a3a37` | n/a | agent card border, closed escalation border | `--border` (ours is one level only) |
| `--border-strong` | `#55544f` | n/a | selected session row, hovered card, composer and note input border | `--input` / `--ring` |
| `--border-escalation` | `#6e4605` | n/a | escalation border, option button border; option hover bg (`--amber-800`) | `--escalation` |
| `--text-primary` | `#f2f1ec` | n/a | body text | `--foreground` |
| `--text-secondary` | `#c9c8c2` | n/a | task lines, tool rows, icon-button idle colour, heads_up note label | none; **add** (`--secondary-foreground` is not used this way) |
| `--text-muted` | `#8f8e89` | n/a | meta, placeholders, labels, icons | `--muted-foreground` |
| `--status-running` | `#34b233` (green) | n/a | running agent, "working" session | `--status-working` (**conflict**: ours is blue) |
| `--status-done` | `#34b233` (green) | n/a | "done" note type only | `--note-done` |
| `--status-attention` | `#e39b0e` (amber) | n/a | "needs you", escalation text, question note | `--status-needs-you(-fg)`, `--escalation-fg`, `--note-question` (**conflict**: ours is violet) |
| `--status-decision` | `#6ea8ef` (blue) | n/a | decision note, links, "you" label | `--note-decision` |
| `--status-blocked` | `#e5655a` (red) | n/a | blocker note | `--note-blocker`, `--status-crashed` |
| `--status-idle` | `#8f8e89` | n/a | defined but unused | `--status-done` |

Colour by status:

| Status | Design colour | Ours |
|---|---|---|
| Done session | `--text-muted` | `--status-done` = muted, matches |
| Done agent ("done · 1m40s") | `--text-muted` | `--status-exited` (green-teal), conflict |
| heads_up note | `--text-secondary` (neutral) | `--note-heads-up` (amber), conflict |

### Type, space, radius, motion
| Token | Value | Used in markup? |
|---|---|---|
| `--font-sans` | `"Instrument Sans", "Helvetica Neue", Helvetica, sans-serif` | yes (ours: Geist Variable) |
| `--font-serif` | `"Source Serif 4", Georgia, serif` | yes (assistant voice) |
| `--font-mono` | `"JetBrains Mono", ui-monospace, Menlo, monospace` | yes (agent stream) |
| `--text-sm` / `--text-base` / `--text-title` / `--text-assistant` | 13 / 14 / 15 / 15 px | values used inline, tokens not referenced |
| `--leading-tight` / `--leading-base` / `--leading-prose` | 18 / 20 / 21 px | assistant text actually uses 22px |
| `--weight-regular` / `--weight-medium` / `--weight-semibold` | 400 / 500 / 600 | inline |
| `--space-1` … `--space-10` | 2, 4, 6, 8, 10, 12, 14, 16, 20 px | not referenced (inline px instead) |
| `--radius-sm` / `-md` / `-lg` / `-xl` | 6 / 8 / 10 / 12 px | 6, 8, 10 used inline; 12 unused (ours: `--radius` 10px; `sm` 6, `md` 8, `lg` 10, `xl` 14) |
| `--border-width` | 1px | yes |
| `--panel-header-h` | 44px | only the Notes header is 44; the other headers are 52 |
| `--sidebar-w` | 160px | unused (the sidebar is 180–220) |
| `--duration-fast` / `--ease-standard` | 120ms / `cubic-bezier(.2,0,0,1)` | unused (no transitions in markup) |
| Shadows | none | none anywhere |

## 2. Typography

The design uses one working size (14px). Hierarchy comes from weight, colour and the serif voice, not from size.

| Role | Family | Size / line | Weight | Colour |
|---|---|---|---|---|
| Wordmark "Fragile" | sans | 16 / 20, `letter-spacing:-0.01em` | 600 | primary |
| Panel / session titles (main header, Agents, Notes, agent view) | sans | 15 / 20 | 600 | primary |
| Body, user msg, notes, cards | sans | 14 / 20 | 400 | primary |
| Agent card name | sans | 14 / 20 | 600 | primary |
| "Escalation" label | sans | 14 / 20 | 500 | amber |
| Small meta ("Sessions" label, option buttons, "or reply below", "Output stream · view only") | sans | 13 / 18–20 | 400 | muted / amber |
| Assistant message | **serif** | 15 / 22, `text-wrap:pretty` | 400 | primary |
| Empty-state headline | **serif** | 18 / 24 | 400 | primary |
| Agent stream tool line | mono | 12 / 20 | 400 | secondary |
| Agent stream timestamp | mono | 11 / 20 | 400 | muted |

Fonts are bundled as Google Fonts variable-font subsets, all SIL OFL 1.1. The copies in `fragile-session-src/fonts/` are latin and latin-ext only. For the app, prefer `@fontsource-variable/instrument-sans`, `@fontsource-variable/source-serif-4` and `@fontsource-variable/jetbrains-mono`, the same pattern as our Geist import. The export notes they are "Substitutes picked from Google Fonts".

Weights loaded:

| Family | Weights |
|---|---|
| Instrument Sans | 400, 500, 600 |
| Source Serif 4 | 400 |
| JetBrains Mono | 400 |

## 3. Layout

- **Grid.** `grid-template-columns: minmax(180px,220px) minmax(0,1fr) minmax(260px,340px)`, `height:100vh`, no resizers.

  | Viewport | Sidebar | Center | Right |
  |---|---|---|---|
  | 1440 | 220 | 880 | 340 |
  | 1280 | 220 | 720 | 340 |
  | 900 | 220 | 340 | 340 |

  The side columns stay at max width and the center absorbs the squeeze. There is no collapse.
- **Header rows.** 52px for the sidebar brand row, main header, Agents header and agent-view header. The Notes header is 44px. Headers are not shaded: they are separated only by a 1px `--border-subtle` bottom border. The sidebar brand row has no border.
- **Dividers.** All 1px `--border-subtle`: sidebar right edge, right column left edge, header bottoms, Agents/Notes split (Notes header `border-top`), agent-view sub-header bottom.
- **Sidebar.** Background `--surface-frame`.
  - Brand row: padding `0 10px 0 16px`, wordmark left, 28px "+" icon button right.
  - "Sessions" label: padding `4px 16px 6px`, 13px muted.
  - List: padding `0 8px 12px`, gap 4px, scrolls.
- **Main.** `grid-template-rows: 52px 1fr auto`.
  - Header: padding `0 24px`, gap 10.
  - Message column: `max-width:680px`, centered, padding `24px 24px 16px`, `gap:14px`.
  - Composer wrapper: padding `0 24px 18px`, same 680 max width. There is no border-top above the composer.
- **Right column.**
  - Agents list: `flex:1 1 55%`, padding 12, gap 8.
  - Notes: `flex:1 1 45%`; list padding `0 16px 16px`, gap 7.
  - Agent view replaces this column (the center keeps the chat).

## 4. Components

### Session sidebar (expanded)
As in Layout. **Collapsed state: not in design.** The repo's earlier `session-layout-v2.html` agreed on a collapsible rail with status dots, and our app already has one.

### Session row
- Structure: `<button>`, two lines, left-aligned.
  - Line 1: title (14, primary, ellipsis).
  - Line 2: status label as plain coloured text.
- Box: padding `6px 10px`, radius **10px**, border 1px transparent. Measures 203x54 at 1440.
- **Selected**: bg `--surface-panel`, border `--border-strong` (an outlined "card" lifted from the darker sidebar).
- **Hover**: not defined for rows (only for icon buttons and cards).

Status labels:

| Status | Colour |
|---|---|
| `working` | green |
| `needs you` | amber |
| `done` | muted |
| `new` | muted (session with no messages) |

There are no dots, badges, pills, accent bars or needs-you counts.

### Main header
- Contents in order: session title (15/600, ellipsis), status label (coloured, same as the sidebar), flex spacer, then muted `orchestrator · 2 running · 1 done` or `orchestrator · no agents`.
- No stop button.

### Orchestrator chat
- **User message**: `justify-self:end`, max-width 80%, bg `--surface-sunken` (`#141413`, darker than the panel), radius 8, padding `6px 12px`, sans 14. No timestamp, no tail.
- **Assistant message**: plain serif 15/22, full column width, no bubble or avatar. This is the "voice" of the orchestrator. Plain text in the design; ours renders Markdown.
- **Tool call row**: flex, gap 6, 13px wrench icon (muted) followed by a one-line sans 14 `--text-secondary` summary (`spawn_subagent x3`). No border, no bg, no mono, no expand.
- **Escalation (open)**:
  - Box: bg `#3a2502`, 1px border `#6e4605`, radius 8, padding `10px 14px`, all text amber (`--status-attention`), `gap:2px`.
  - Line 1: **Escalation** (500) followed by `from #2 storage` at opacity .75.
  - Line 2: the question.
  - Line 3 (margin-top 8, gap 6): one **option button** per suggested answer, plus "or reply below" (13px, opacity .75).
    - Option button: 13/18, amber text, transparent bg, 1px `#6e4605` border, radius 6, padding `3px 10px`.
    - Option hover: bg `#6e4605`, text primary.
  - **There is no answer box inside the block.** Free-text answers go through the main composer, whose placeholder becomes "Answer the escalation, or message the orchestrator". The next message sent answers the open escalation.
- **Escalation (answered)**:
  - Box: 1px `--border-default` border, no bg, radius 8, padding `8px 14px`.
  - Line 1, muted: `Escalation from #2 storage · answered`.
  - Line 2: question in `--text-secondary`.
  - Line 3: `you` in blue (`--status-decision`) followed by the answer.
  - The orchestrator then replies, and a `decision` note by `you` is posted.
- **Empty session**: centered, padding 48 0. Serif 18/24 "What should this session build?" over a muted "Give the orchestrator a task. It will split it up and spawn sub-agents."

### Composer
- Single-line `<input>`, full 680 width, 44px tall.
- Box: bg `--surface-sunken`, 1px `--border-strong`, radius **10**, padding `11px 14px`, no outline.
- Focus: border becomes `--text-muted`.
- No send button and no hint text. Enter sends.
- Placeholder by state:

  | State | Placeholder |
  |---|---|
  | Empty session | "Describe the task" |
  | Escalation open | "Answer the escalation, or message the orchestrator" |
  | Otherwise | "Message the orchestrator" |

### Agents panel header
52px, padding `0 16px`, border-bottom. "Agents" (15/600) on the left; muted summary on the right (`2 running · 1 done` / `no agents`). Empty state: muted "Sub-agents appear here once the orchestrator spawns them." with no dashed box.

### Agent card
- Structure: `<button>`, transparent bg, 1px `--border-default`, radius 8, padding `8px 12px`. Measures 315x78 at 1440. Three lines, no gap:
  1. `#2 storage package` (600, ellipsis) on the left; status on the right (nowrap).
  2. Task description, `--text-secondary`, ellipsis.
  3. A 13px muted icon followed by the latest activity, muted, ellipsis.

     | Latest activity | Icon |
     |---|---|
     | Tool | `terminal` |
     | Text | `message-square` |
     | Note | the note-type icon, with `type: text` as the line |
- **Hover**: bg `--surface-hover`, border `--border-strong` (shot 03).

States:

| State | In design | Look |
|---|---|---|
| Running | yes | green `running · 2m21s` (live clock, format `2m05s`) |
| Done | yes | muted `done · 1m40s` |
| Exited | no | presumably the same as done |
| Crashed | no | none |
| Selected | no | clicking opens the agent view instead |
| Lead / orchestrator | no | the orchestrator is not listed as a card; it lives in the header summary |

### Agent view (right column, replaces Agents + Notes)
- **Header** (52px, padding `0 16px 0 8px`): 28px back button (arrow-left), `#2 storage package` 15/600, spacer, status.
- **Sub-header** (padding `10px 16px`, border-bottom): task (secondary), then "Output stream · view only" (13, muted).
- **Stream**: padding `10px 16px 16px`, rows gap 6, each row a `40px | 1fr` grid with gap 8.
  - Time: mono 11 muted, relative to agent start (`1m10s`).
  - Text lines: sans 14, primary.
  - Tool lines: mono 12, secondary, with a muted `› ` prefix.
  - Note lines: a coloured type word (`question `), then the text.
  - Running agents end with a muted `streaming…` row.
- The design puts this view in the **right column**. Ours replaces the **center** chat.

### Notes panel
- **Header**: 44px, padding `0 10px 0 16px`, border-top. "Notes" (15/600) and a 28px "+" icon button.
- **Note row**: one inline line, wrapping, `column-gap:.3em`:
  1. Type word in its colour, raw (`heads_up`, not "heads up").
  2. Author, muted (`orchestrator`, `#2`, `you`).
  3. Text, primary.

  No badge bg, no card, no border, no timestamp. Newest on top.

  | Type | Colour | Icon (in agent card) |
  |---|---|---|
  | decision | blue `#6ea8ef` | gavel |
  | question | amber `#e39b0e` | circle-help |
  | blocker | red `#e5655a` | octagon-alert |
  | heads_up | `--text-secondary` `#c9c8c2` | sticky-note |
  | done | green `#34b233` | check |
- **Resolved**: not in design.
- **Add note**: "+" toggles an inline input at the top of the list instead of a dialog.
  - Input: placeholder "Post a decision to all agents", transparent bg, 1px `--border-strong`, radius 8, padding `5px 10px`, focus border `--text-muted`.
  - Enter posts the note as type `decision` by `you`; Esc closes. There is no type picker.

### Buttons
- **Icon button**: 28x28, radius 6, no border, transparent, colour `--text-secondary`, 16px icon. Hover: bg `--surface-hover`, colour primary.
- **Option button**: the escalation option described under Escalation (open).
- No primary, filled, destructive or outline buttons in the design.

### Inputs
Composer and note input only, both described above. No textarea, select or focus ring (focus is only a border-colour change).

### Dialogs
Not in design. New session is created instantly as "New session" with no directory picker. There is no stop confirmation and no add-note dialog.

### Badges
None. Every status is coloured plain text.

### Icons
**Lucide** (`lucide-static` v0.460.0, ISC), rendered as CSS masks with `background: currentColor`.

| Where | Icons |
|---|---|
| Static markup | `plus`, `arrow-left`, `wrench` |
| Agent card latest line | `terminal`, `message-square`, `gavel`, `circle-help`, `octagon-alert`, `sticky-note`, `check` |

We already use `lucide-react`, so these map 1:1.

## 5. Interaction states seen

| Element | Hover | Selected / active | Focus |
|---|---|---|---|
| Session row | none | bg panel, border strong | none |
| Agent card | bg hover, border strong | n/a (opens view) | none |
| Icon button | bg hover, text primary | n/a | none |
| Escalation option | bg amber-800, text primary | n/a | none |
| Composer / note input | n/a | n/a | border becomes `--text-muted` |

- No transitions, no `focus-visible` rings and no disabled states.
- Live behaviour: agent elapsed clocks tick every second, and the running agent's latest line updates.

## 6. Design vs our app

**In the design, missing from our app or data model:**
- **Escalation options** (suggested answers as buttons). `Escalation` has only `question`, `context`, `status` and `answer`. This needs an `options []string` field (Go `notes/store.go` plus the escalation tool schema).
- **Answering through the main composer** while an escalation is open. Ours has an inline textarea and button inside the block.
- **Auto-posting a `decision` note** when the user answers. We would need to check whether the backend does this.
- **Agent card name and task as two lines** (`#2 storage package` / `Store interface + file backend`). These map to `Task.title` / `Task.description`; `#id` is `Agent.id`. Ours shows the title, then status, then description.
- **"from #2 storage"** on escalations. This is derivable from `agent_id` plus the task title.
- **Session status `new`** (no messages yet). There is no backend status for it; it could be derived client-side.
- **Header agent summary** (`orchestrator · 2 running · 1 done`). Derivable.
- **Agent stream with relative timestamps** and a `streaming…` row. Derivable from `AgentEvent.created_at` minus `Agent.created_at`.
- **Note `author` as `#id`** instead of the agent name. Display only.
- **Agent view in the right column**, keeping the chat visible.
- **Fonts**: Instrument Sans, Source Serif 4 (assistant voice) and JetBrains Mono. Ours uses Geist only.
- **Warm neutral palette** and a green "running" colour. Ours is pure grey with a blue "working" colour.
- **Empty-session prompt** ("What should this session build?") and an instant "New session".

**In our app, not in the design (keep, and style in the design's language):**
- Light theme (`:root` values). The design is dark-only.
- Collapsible sidebar rail with status dots, the Cmd+B shortcut, and resizable panes with persisted sizes.
- Needs-you count in the sidebar header, and needs-you row tint and left accent bar.
- Agent `crashed` state and `exited` naming, the orchestrator card with a `lead` badge, and the selected-card state.
- Note resolve/reopen (`NoteStatus`), note timestamps, note type picker, add-note dialog.
- New-session dialog with task text and working-directory picker (`work_dir`).
- Stop session button with a confirmation dialog.
- Markdown and Shiki rendering in assistant and agent output, and collapsible tool_use/tool_result folds.
- Escalation `context` line.
- User-message timestamps.
- Composer: multiline textarea, send button, disabled-reason text and error text.
- Toasts.
- Tasks with `planned`, `blocked` and `review` status (not surfaced in either).
