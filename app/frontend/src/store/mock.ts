// MOCK DATA: demo seed for the UI shell. Delete this file (and its use in store/app.ts) when the
// Go bindings (#15/#17) feed the store. Nothing else imports it.
import type {Agent, AgentEvent, ChatItem, Note, Session, Task} from '@/lib/types'
import type {SessionData} from '@/store/app'

const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString()

const CODE = '```ts\nexport function requireAuth(next: Handler): Handler {\n  return async (req, res) => {\n    const token = req.headers.authorization?.slice(7)\n    if (!token || !(await verify(token))) return res.status(401).end()\n    return next(req, res)\n  }\n}\n```'

// Believable sub-agent log: read -> grep -> edit -> test cycles over a fixed set of files.
const FILES = ['router.ts', 'middleware/auth.ts', 'middleware/session.ts', 'routes/users.ts', 'routes/billing.ts', 'routes/admin.ts', 'lib/token.ts', 'lib/cookies.ts', 'handlers/login.ts', 'handlers/logout.ts', 'test/auth.test.ts', 'test/router.test.ts']
const THOUGHTS = [
  (f: string) => `Reading \`${f}\` to see how it wires into the old middleware.`,
  (f: string) => `\`${f}\` still imports the v1 helper. I'll switch it to the new router API.`,
  (f: string) => `Nothing to change in \`${f}\`; it only depends on the request type.`,
  (f: string) => `Updated \`${f}\`. Running the auth tests before moving on.`,
]
const RESULTS = [ // one per tool in bigLog's cycle: Read, Grep, Edit
  (f: string) => `import {requireAuth} from './auth'\nimport {Router} from '../router'\n\nexport const routes = (r: Router) => {\n  r.use(requireAuth)\n}\n// ${f}`,
  () => 'src/router.ts:41:  legacyAuth(req, res, next)\nsrc/routes/users.ts:12:  legacyAuth(req, res, next)',
  (f: string) => `The file src/${f} has been updated.`,
]

function bigLog(agentId: number, n: number): AgentEvent[] {
  const out: AgentEvent[] = []
  for (let i = 0; i < n; i++) {
    const f = FILES[(i >> 2) % FILES.length]
    const step = i % 4
    const [event_type, payload] =
      step === 0 ? ['assistant_text', {text: i % 200 === 0 ? `Here is the target shape for \`${f}\`:\n\n${CODE}` : THOUGHTS[(i >> 2) % THOUGHTS.length](f)}]
      : step === 1 ? ['tool_use', ((i >> 2) % 3 === 0 ? {name: 'Read', input: {file_path: `src/${f}`}} : (i >> 2) % 3 === 1 ? {name: 'Grep', input: {pattern: 'legacyAuth', path: 'src'}} : {name: 'Edit', input: {file_path: `src/${f}`, old_string: 'legacyAuth', new_string: 'requireAuth'}})]
      : step === 2 ? ['tool_result', {content: RESULTS[(i >> 2) % 3](f)}]
      : ['assistant_text', {text: `Step ${(i >> 2) + 1} of the migration done for \`${f}\`.`}]
    out.push({id: i + 1, agent_id: agentId, event_type, payload: JSON.stringify(payload), created_at: ago(90 - (i * 80) / n)})
  }
  return out
}

function smallLog(agentId: number, lines: string[]): AgentEvent[] {
  const ev = (id: number, event_type: string, payload: object, min: number): AgentEvent =>
    ({id: 100_000 + agentId * 100 + id, agent_id: agentId, event_type, payload: JSON.stringify(payload), created_at: ago(min)})
  return [
    ev(1, 'assistant_text', {text: lines[0]}, 20),
    ev(2, 'tool_use', {name: 'Glob', input: {pattern: 'docs/**/*.md'}}, 19),
    ev(3, 'tool_result', {content: 'docs/index.md\ndocs/api.md'}, 19),
    ev(4, 'assistant_text', {text: lines[1]}, 18),
  ]
}

const asst = (id: number, text: string, min: number): ChatItem => ({id, kind: 'assistant', text, at: ago(min)})
const user = (id: number, text: string, min: number): ChatItem => ({id, kind: 'user', text, at: ago(min)})
const tool = (id: number, name: string, summary: string, min: number): ChatItem => ({id, kind: 'tool', name, summary, at: ago(min)})

export function mockState() {
  // First session is the initially selected one (see store/app.ts).
  const sessions: Session[] = [
    {id: 1, title: 'Refactor auth middleware', status: 'working', created_at: ago(95)},
    {id: 2, title: 'Write API documentation', status: 'needs_you', created_at: ago(120)},
    {id: 3, title: 'Fix flaky CI on main', status: 'done', created_at: ago(300)},
  ]

  // Session 1: orchestrator + 3 sub-agents, one with a 12k-event log.
  const s1: SessionData = {
    agents: [
      {id: 10, session_id: 1, role: 'orchestrator', status: 'running', created_at: ago(95)},
      {id: 11, session_id: 1, parent_id: 10, role: 'subagent', task_id: 1, status: 'running', created_at: ago(90)},
      {id: 12, session_id: 1, parent_id: 10, role: 'subagent', task_id: 2, status: 'exited', exit_code: 0, created_at: ago(80), exited_at: ago(41)},
      {id: 13, session_id: 1, parent_id: 10, role: 'subagent', task_id: 3, status: 'crashed', exit_code: 1, created_at: ago(60), exited_at: ago(52)},
    ] satisfies Agent[],
    tasks: [
      {id: 1, session_id: 1, title: 'router migration', description: 'Move every route onto the v2 auth middleware and delete the old one.', status: 'working', agent_id: 11},
      {id: 2, session_id: 1, title: 'token expiry tests', description: 'Cover expired, malformed and missing tokens.', status: 'done', agent_id: 12},
      {id: 3, session_id: 1, title: 'session cookies', description: 'Switch cookie signing to the new key format.', status: 'blocked', agent_id: 13},
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
      asst(2, `I read through \`router.ts\` and the three middleware files. This splits cleanly into three independent tasks, so I'll run them in parallel:\n\n1. Port every route to the v2 middleware\n2. Add token expiry tests\n3. Migrate session cookies to the new key format\n\nTarget shape of the middleware:\n\n${CODE}`, 94),
      tool(3, 'spawn_subagent', 'router migration', 90),
      tool(4, 'spawn_subagent', 'token expiry tests', 80),
      tool(5, 'spawn_subagent', 'session cookies', 60),
      asst(6, 'Posted decision #1: the legacy `/login` route stays until clients migrate, so the router migration can leave it alone.', 88),
      user(7, 'Fine. Also check whether the mobile app still sends v1 tokens.', 70),
      tool(8, 'note_post', 'question: mobile app v1 tokens', 69),
      asst(9, 'Expiry tests are merged (14 cases). The cookie migration crashed because the staging secret is missing; I posted a blocker note and will retry once it is available.', 52),
      tool(10, 'read_notes', '5 open', 12),
      asst(11, 'The router migration is about 70% done. I will rerun the full suite when it finishes.', 5),
    ],
  }

  const s2: SessionData = {
    agents: [
      {id: 20, session_id: 2, role: 'orchestrator', status: 'running', created_at: ago(120)},
      {id: 21, session_id: 2, parent_id: 20, role: 'subagent', task_id: 4, status: 'running', created_at: ago(25)},
    ],
    tasks: [{id: 4, session_id: 2, title: 'endpoints reference', description: 'Document every public endpoint with request/response examples.', status: 'working', agent_id: 21}],
    notes: [
      {id: 7, board_id: 2, author_agent_id: 21, type: 'heads_up', content: 'Found 6 admin endpoints in the router.', status: 'open', created_at: ago(11), updated_at: ago(11)},
    ],
    chat: [
      user(2001, 'Write API documentation for the public endpoints.', 120),
      tool(2002, 'spawn_subagent', 'endpoints reference', 25),
      asst(2003, 'Starting with the endpoint reference. I need one decision before I go further.', 24),
      {id: 2004, kind: 'escalation', at: ago(10), escalation: {
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
    agentEvents: {
      11: bigLog(11, 12_000),
      12: smallLog(12, ['Looking at how the existing tests handle tokens first.', 'Drafting the expiry cases: expired, malformed, missing.']),
      21: smallLog(21, ['Looking at the existing docs layout first.', 'Drafting the **endpoints** section now.']),
    } as Record<number, AgentEvent[]>,
    nextId: 10_000,
  }
}
