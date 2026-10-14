// Kanban column placement (src/lib/board.ts), transpiled with the TypeScript dev dependency.
const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const ts = require('typescript')

const source = fs.readFileSync(path.join(__dirname, '../src/lib/board.ts'), 'utf8')
const compiled = ts.transpileModule(source, {compilerOptions: {module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022}}).outputText
const mod = {}
vm.runInNewContext(compiled, {exports: mod, require})
const {boardCards} = mod

const task = (id, status = 'working') => ({id, status, title: `t${id}`})
const sub = (id, task_id, status = 'running') => ({id, task_id, status, role: 'subagent'})
const note = (author_agent_id, type, status = 'open') => ({author_agent_id, type, status})
const col = (cards, id) => cards.find((c) => c.task.id === id).column

const tasks = [task(1), task(2), task(3, 'done'), task(4), task(5), task(6, 'blocked'), task(7)]
const agents = [
  {id: 1, role: 'orchestrator', status: 'running'},
  sub(10, 1),
  sub(20, 2), // open blocker note
  sub(30, 3, 'exited'),
  sub(40, 4, 'crashed'), sub(41, 4), // restarted: latest agent wins
  sub(50, 5, 'exited'), // exited without a done note
  sub(60, 6),
  // task 7 has no agent: no card
]
const notes = [note(20, 'blocker'), note(20, 'question'), note(10, 'blocker', 'resolved'), note(10, 'heads_up'), note(30, 'blocker')]
const cards = boardCards(tasks, agents, notes)

assert.equal(cards.length, 6)
assert.equal(col(cards, 1), 'working')
assert.equal(col(cards, 2), 'blocked')
assert.equal(cards.find((c) => c.task.id === 2).open, 2)
assert.equal(cards.find((c) => c.task.id === 1).open, 0)
assert.equal(col(cards, 3), 'done') // done wins over an open blocker
assert.equal(col(cards, 4), 'working')
assert.equal(cards.find((c) => c.task.id === 4).agent.id, 41)
assert.equal(col(cards, 5), 'blocked')
assert.equal(col(cards, 6), 'blocked')
assert.ok(!cards.some((c) => c.agent.role === 'orchestrator'))
console.log('board ok')
