// MOCK DATA: demo seed for the UI shell. Delete this file (and its use in store/app.ts) when the
// Go bindings (#15/#17) feed the store. Nothing else imports it.
import type {Agent, AgentEvent, ChatItem, Note, Session, Task} from '@/lib/types'
import type {SessionData} from '@/store/app'

const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString()

const CODE = '```ts\nexport function requireAuth(next: Handler): Handler {\n  return async (req, res) => {\n    const token = req.headers.authorization?.slice(7)\n    if (!token || !(await verify(token))) return res.status(401).end()\n    return next(req, res)\n  }\n}\n```'

function bigLog(agentId: number, n: number): AgentEvent[] {
  const out: AgentEvent[] = []
  for (let i = 0; i < n; i++) {
    const step = i % 4
    const [event_type, payload] =
      step === 0 ? ['assistant_text', {text: `Step ${i / 4 + 1}: reading the next module and checking its call sites.`}]
      : step === 1 ? ['tool_use', {name: 'Read', input: {file_path: `src/module${i}.ts`}}]
      : step === 2 ? ['tool_result', {content: `export const value${i} = ${i}\n`.repeat(6)}]
      : ['assistant_text', {text: i % 40 === 3 ? `Found a usage worth changing:\n\n${CODE}` : `Module ${i} looks fine, moving on.`}]
    out.push({id: i + 1, agent_id: agentId, event_type, payload: JSON.stringify(payload), created_at: ago(90 - (i * 80) / n)})
  }
  return out
}

function smallLog(agentId: number): AgentEvent[] {
  const ev = (id: number, event_type: string, payload: object, min: number): AgentEvent =>
    ({id: 100_000 + agentId * 100 + id, agent_id: agentId, event_type, payload: JSON.stringify(payload), created_at: ago(min)})
  return [
    ev(1, 'assistant_text', {text: 'Looking at the existing docs layout first.'}, 20),
    ev(2, 'tool_use', {name: 'Glob', input: {pattern: 'docs/**/*.md'}}, 19),
    ev(3, 'tool_result', {content: 'docs/index.md\ndocs/api.md'}, 19),
    ev(4, 'assistant_text', {text: 'Drafting the **endpoints** section now.'}, 18),
  ]
}

const asst = (id: number, text: string, min: number): ChatItem => ({id, kind: 'assistant', text, at: ago(min)})
const user = (id: number, text: string, min: number): ChatItem => ({id, kind: 'user', text, at: ago(min)})
const tool = (id: number, name: string, summary: string, min: number): ChatItem => ({id, kind: 'tool', name, summary, at: ago(min)})

export function mockState() {
  const sessions: Session[] = [
    {id: 3, title: 'Fix flaky CI on main', status: 'done', created_at: ago(300)},
    {id: 2, title: 'Write API documentation', status: 'needs_you', created_at: ago(120)},
    {id: 1, title: 'Refactor auth middleware', status: 'working', created_at: ago(95)},
  ]

  // Session 1: orchestrator + 3 sub-agents, one with a 12k-event log.
  const longChat: ChatItem[] = []
  for (let i = 0; i < 150; i++) {
    longChat.push(i % 3 === 0 ? user(1000 + i, `Follow-up question ${i / 3 + 1}: what about the edge cases?`, 90 - i / 2)
      : i % 3 === 1 ? tool(1000 + i, 'note_post', 'posted heads_up', 90 - i / 2)
      : asst(1000 + i, `Good point. I'll cover that case too.\n\n- checked token expiry\n- checked missing header`, 90 - i / 2))
  }
  const s1: SessionData = {
    agents: [
      {id: 10, session_id: 1, role: 'orchestrator', status: 'running', created_at: ago(95)},
      {id: 11, session_id: 1, parent_id: 10, role: 'subagent', task_id: 1, status: 'running', created_at: ago(90)},
      {id: 12, session_id: 1, parent_id: 10, role: 'subagent', task_id: 2, status: 'exited', exit_code: 0, created_at: ago(80), exited_at: ago(41)},
      {id: 13, session_id: 1, parent_id: 10, role: 'subagent', task_id: 3, status: 'crashed', exit_code: 1, created_at: ago(60), exited_at: ago(52)},
    ] satisfies Agent[],
    tasks: [
      {id: 1, session_id: 1, title: 'Port middleware to new router', description: 'Move every route onto the v2 auth middleware and delete the old one.', status: 'working', agent_id: 11},
      {id: 2, session_id: 1, title: 'Add token expiry tests', description: 'Cover expired, malformed and missing tokens.', status: 'done', agent_id: 12},
      {id: 3, session_id: 1, title: 'Migrate session cookies', description: 'Switch cookie signing to the new key format.', status: 'blocked', agent_id: 13},
    ] satisfies Task[],
    notes: [
      {id: 1, board_id: 1, author_agent_id: 10, type: 'decision', content: 'Keep the legacy `/login` route until clients migrate.', status: 'open', created_at: ago(88), updated_at: ago(88)},
      {id: 2, board_id: 1, author_agent_id: 13, type: 'blocker', content: 'Cookie key rotation needs the staging secret.', status: 'open', created_at: ago(55), updated_at: ago(55)},
      {id: 3, board_id: 1, author_agent_id: 12, type: 'done', content: 'Expiry tests merged: 14 cases.', status: 'resolved', created_at: ago(41), updated_at: ago(40)},
      {id: 4, board_id: 1, author_agent_id: 11, type: 'heads_up', content: 'Touching `router.ts`; avoid parallel edits.', status: 'open', created_at: ago(30), updated_at: ago(30)},
      {id: 5, board_id: 1, author_agent_id: 0, type: 'question', content: 'Do we need to support the mobile app v1 tokens?', status: 'open', created_at: ago(12), updated_at: ago(12)},
    ] satisfies Note[],
    chat: [
      user(1, 'Refactor the auth middleware to the new router and keep tests green.', 95),
      asst(2, `I'll split this into three tasks:\n\n1. Port routes to the v2 middleware\n2. Add token expiry tests\n3. Migrate session cookies\n\nHere is the target shape of the middleware:\n\n${CODE}`, 94),
      tool(3, 'spawn_subagent', 'Port middleware to new router', 90),
      tool(4, 'spawn_subagent', 'Add token expiry tests', 80),
      asst(5, 'Tests are in. The cookie migration crashed; I\'m investigating and will post a blocker note.', 52),
      ...longChat,
    ],
  }

  const s2: SessionData = {
    agents: [
      {id: 20, session_id: 2, role: 'orchestrator', status: 'running', created_at: ago(120)},
      {id: 21, session_id: 2, parent_id: 20, role: 'subagent', task_id: 4, status: 'running', created_at: ago(25)},
    ],
    tasks: [{id: 4, session_id: 2, title: 'Draft endpoints reference', description: 'Document every public endpoint with request/response examples.', status: 'working', agent_id: 21}],
    notes: [],
    chat: [
      user(2001, 'Write API documentation for the public endpoints.', 120),
      asst(2002, 'Starting with the endpoint reference. I need one decision before I go further.', 24),
      {id: 2003, kind: 'escalation', at: ago(10), escalation: {
        id: 1, session_id: 2, agent_id: 20, status: 'open',
        question: 'Should the docs include the internal `/admin` endpoints?',
        context: 'Found 6 admin endpoints in the router. They are not covered by the public API policy.',
      }},
    ],
  }

  const s3: SessionData = {
    agents: [{id: 30, session_id: 3, role: 'orchestrator', status: 'exited', exit_code: 0, created_at: ago(300), exited_at: ago(240)}],
    tasks: [],
    notes: [{id: 6, board_id: 3, author_agent_id: 30, type: 'done', content: 'Flaky test was a race in the fixture teardown; fixed.', status: 'resolved', created_at: ago(245), updated_at: ago(245)}],
    chat: [user(3001, 'CI is flaky on main, find out why.', 300), asst(3002, 'Root cause: fixture teardown race. Fixed and verified over 20 runs.', 245)],
  }

  return {
    sessions,
    data: {1: s1, 2: s2, 3: s3} as Record<number, SessionData>,
    agentEvents: {11: bigLog(11, 12_000), 12: smallLog(12), 21: smallLog(21)} as Record<number, AgentEvent[]>,
    selectedSessionId: 1,
    nextId: 10_000,
  }
}
