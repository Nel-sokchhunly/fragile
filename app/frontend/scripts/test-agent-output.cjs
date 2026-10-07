// Exercise the actual Zustand store with deterministic deferred native replies.
// TypeScript is already a direct dev dependency; no additional test runner needed.
const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const ts = require('typescript')

const source = fs.readFileSync(path.join(__dirname, '../src/store/app.ts'), 'utf8')
const compiled = ts.transpileModule(source, {compilerOptions: {module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022}}).outputText
const event = (id, agent_id = 7) => ({id, agent_id, event_type: 'assistant_text', payload: JSON.stringify({text: String(id)}), created_at: ''})
function fixture() {
  const pending = []
  const api = {getAgentEventTail: (id, limit) => new Promise((resolve, reject) => pending.push({id, limit, resolve, reject}))}
  const exports = {}
  vm.runInNewContext(compiled, {exports, require: (name) => name === '@/lib/api' ? {api} : require(name), setTimeout: () => 0, console})
  const store = exports.useAppStore
  store.setState({sessions: [{id: 1}], data: {1: {agents: [{id: 7}], tasks: [], notes: [], chat: []}}})
  return {store, pending}
}
const ids = (store) => Array.from(store.getState().agentEvents[7] ?? [], (e) => e.id)

async function run() {
  // A single bounded request, live events during loading, deduplication, order and cap.
  {
    const {store, pending} = fixture()
    const load = store.getState().loadAgentEvents(7)
    await store.getState().loadAgentEvents(7)
    assert.equal(pending.length, 1)
    assert.equal(pending[0].limit, 2000)
    store.getState().agentEvent(1, event(4002))
    store.getState().agentEvent(1, event(4000))
    pending[0].resolve(Array.from({length: 2000}, (_, i) => event((i + 1) * 2)))
    await load
    const result = ids(store)
    assert.equal(result.length, 2000)
    assert.equal(result[0], 4)
    assert.equal(result.at(-1), 4002)
    assert.equal(new Set(result).size, result.length)
    assert.equal(store.getState().agentLoaded[7], true)
  }
  // Failed native reads remain retryable; the retry still captures buffered live output.
  {
    const {store, pending} = fixture()
    const failed = store.getState().loadAgentEvents(7)
    pending[0].reject('read failed')
    await failed
    assert.equal(store.getState().agentEvents[7], undefined)
    assert.equal(store.getState().agentLoaded[7], undefined)
    assert.equal(store.getState().toasts.length, 1)
    const retry = store.getState().loadAgentEvents(7)
    store.getState().agentEvent(1, event(8))
    pending[1].resolve([event(6)])
    await retry
    assert.deepEqual(ids(store), [6, 8])
  }
  // SQLite may reuse agent IDs. An old reply must neither resurrect the deleted
  // session nor overwrite/clear a new same-ID load; test success and failure replies.
  for (const rejectOld of [false, true]) {
    const {store, pending} = fixture()
    const old = store.getState().loadAgentEvents(7)
    store.getState().sessionDeleted(1)
    assert.equal(store.getState().agentEvents[7], undefined)
    store.setState({sessions: [{id: 2}], data: {2: {agents: [{id: 7}], tasks: [], notes: [], chat: []}}})
    const fresh = store.getState().loadAgentEvents(7)
    if (rejectOld) pending[0].reject('stale failure')
    else pending[0].resolve([event(999)])
    await old
    assert.deepEqual(ids(store), [])
    assert.equal(store.getState().agentLoaded[7], undefined)
    assert.equal(store.getState().toasts.length, 0)
    pending[1].resolve([event(10)])
    await fresh
    assert.deepEqual(ids(store), [10])
    assert.equal(store.getState().agentLoaded[7], true)
  }
  // Empty successful history is loaded, not repeatedly fetched.
  {
    const {store, pending} = fixture()
    const load = store.getState().loadAgentEvents(7)
    pending[0].resolve([])
    await load
    await store.getState().loadAgentEvents(7)
    assert.equal(pending.length, 1)
    assert.equal(store.getState().agentLoaded[7], true)
  }
  console.log('agent output deferred-load regression checks passed')
}
run().catch((error) => { console.error(error); process.exitCode = 1 })
