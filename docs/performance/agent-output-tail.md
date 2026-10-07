# Bounded agent-output retrieval

## Journey, scope and mechanism

Journey: click an existing agent card → its newest output becomes visible and usable. Prioritization is an assumption: no real-user usage telemetry is available. The frontend has always displayed at most the newest 2,000 events, but previously fetched the whole history in oldest-first pages of 1,000 before setting `agentLoaded`. This change requests one bounded tail snapshot and merges the existing bounded live-event buffer.

Reuse the existing SQLite `(agent_id, id)` index: reverse scan at most 2,000 rows, reverse the returned slice into chronological order. No dependency, migration, flag or new index. The original paginated `GetAgentEvents` API is unchanged, including its 1,000-row ceiling. Full history remains stored and available through that API. The new `GetAgentEventTail` defaults/caps at 2,000.

## Measurement boundary and baseline

Baseline source: upstream main `27094f3374592e044b6b9e0f534709fd6b1e63e8`, including PR52/53. The old `ListAgentEvents` implementation is retained unchanged. Both old paged and new tail paths run in the **same final test binary**, with the benchmark reproducing the store reads and retained 2,000-row result of the old loader. This is a path comparison, not timings from two complete desktop app builds.

Before collecting timings, `go test ./notes ./app` established a runnable Go checkout. Existing Go benchmarks, SQLite query-plan checks, existing migration/index and existing frontend TypeScript tooling were reused. No extra profiling library or test runner was added.

Environment: macOS Darwin ARM64, Apple M4, Go 1.26.0. Local temporary WAL SQLite database, single connection, synthetic uniform assistant-text events (about 150-byte JSON payload). Fixtures are seeded transactionally outside timing; 20-row control, 2,000, 10,000 and 100,000 rows. Local network-free warm-cache store reads. Go adaptive benchmark calibration warms the path and is excluded from its reported iteration timing. Eight samples per path/fixture, 200 ms target per adaptive run; process invocation order alternates paged→tail and tail→paged each pair. The benchmark is identical across compared paths. Background machine load is uncontrolled; retained ranges disclose noise.

**Excluded:** `FindAgent` lookups, Wails IPC/serialization, JavaScript concatenation/dedup/sort, React rendering, native click-to-usable-output timing, auth and network. The counter instruments completed store read calls and returned rows, not a SQL-driver trace. Each store read issues one SELECT by source inspection; the App also performs one agent lookup per API call. SQL round trips at the App boundary are therefore source-derived, not instrumented here. No desktop interaction, native speedup or field-performance claim is made.

Numbers below are medians/ranges of **adaptive-run mean ns/op**, not individual-request percentiles. No p95 claim.

| History rows | Paged ms median [range] | Tail ms median [range] | Change | Store calls old/new | Rows read old/new |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 20 | 0.0337 [0.0331, 0.0362] | 0.0324 [0.0321, 0.0363] | -3.8% | 1/1 | 20/20 |
| 2000 | 1.3119 [1.3023, 1.5417] | 1.4841 [1.4752, 1.6125] | +13.1% | 3/1 | 2000/2000 |
| 10000 | 7.0121 [6.9330, 8.2767] | 1.5156 [1.4914, 1.5562] | -78.4% | 11/1 | 10000/2000 |
| 100000 | 70.8932 [70.4482, 79.4073] | 1.6240 [1.6110, 1.7144] | -97.7% | 101/1 | 100000/2000 |

The 20-row control difference is within overlapping observed variability, not a claimed win. At exactly 2,000 rows the store portion **regresses about 0.172 ms / 13.1%**: descending row retrieval/reversal is slower than the old ascending scan in this fixture. It still removes two API invocations, whose end-to-end effect is not measured. This tradeoff is explicit; do not present the optimization as universally faster. Long histories show meaningful bounded-work gains: 10,000 rows −78.4%; 100,000 rows −97.7%, with the same newest-2,000 retained output. No native acceleration is inferred from those percentages.

| History rows | Paged / tail median bytes per operation | Paged / tail median allocations |
| ---: | ---: | ---: |
| 20 | 14,536 / 13,096 | 191 / 190 |
| 2000 | 1,289,168 / 1,182,356 | 17,843 / 17,782 |
| 10000 | 7,794,050 / 1,184,400 | 90,136 / 18,037 |
| 100000 | 86,617,048 / 1,184,398 | 903,387 / 18,037 |

## Correctness and regression protection

- Newest 2,000 identities and chronological order are tested on 2,007 events with unrelated agent events interleaved; no assumption that global IDs are contiguous.
- Store tests verify cross-session isolation, cap/default/negative/oversized limit, empty history and deletion of the newest event. Query-plan regression verifies existing indexed access and no temporary sort.
- App tests verify empty JSON-compatible array, missing-agent rejection, newest-tail semantics and continued old pagination behavior.
- The actual Zustand store is transpiled by the existing TypeScript dependency and exercised with deterministic deferred native replies: single bounded request, live-buffer merge/dedup/order/cap, failed-read retry, empty-history caching and stale success/failure during deletion and same-ID reuse. Load-specific tokens prevent stale replies from resurrecting deleted output or clearing a new load.
- CI keeps timings out of noisy gates; Go race/vet and the deferred frontend regression script protect stable semantics. `pnpm test:agent-output` is added to the existing frontend CI command.

Full local `go vet ./...`, `go test -race ./...`, frozen-lock frontend installation, TypeScript, deferred-load tests and Vite build pass. Existing large-bundle warning remains. Actual native interaction/runtime verification remains unmeasured; this backend-only result must not be described as full journey completion.

## Reproduction and exact provenance

Run from repository root:

```sh
go test -c ./notes -o /tmp/fragile-agent-output.test
/tmp/fragile-agent-output.test -test.run '^$' -test.bench BenchmarkAgentOutputRetrieval -test.benchmem -test.benchtime=200ms -test.count=1
```

Repeat eight pairs, reversing the paged/tail process order on alternating pairs as in retained `measure.py`. Retained local evidence: `artifacts/fragile-output-perf-20261008/{measure.py,final-retrieval.txt,statistics.json,hashes.json}`, baseline source snapshots and exact binary `artifacts/fragile-output-perf-20261008.test`. Pilot `retrieval.txt` is superseded by final alternating measurements.

SHA-256 of the final measured files and binary:

- `notes/store.go`: `3b2586586b62528808b1bd45b76d7227bb2c0bcc1a6e8c89a0a247b4ac2a12ff`
- `app/app.go`: `71546403f0c04d4a8fabf2658616f7d3680d10fe730efc234332bc7c7204cbf3`
- `app/frontend/src/lib/api.ts`: `288b0313135e6a162fd94bf5562197eef1cfd33de908d2f154737dbfedaa5009`
- `app/frontend/src/store/app.ts`: `247d463bb078d3ea1dde7bb4cc99dab6d4fa5f08ecf47ac430dd96faf8ef123a`
- `notes/agent_tail_test.go`: `80c3131951583f2fffe7373f7c374cbb55d8206173be1e49e337eff4df3b2bd5`
- `test_binary`: `8b2eded87bdd28f3ba13daf4b9fb4ead22b0235ab3caa0bef4267fa8f5844115`
- `benchmark_function`: `ce0c5aebf695098e44bfab58950a81d349f4ec9018105a15119e0e8a446d6ac2`

No production changes were made after the final binary was compiled and remeasured. Documentation/commit metadata do not change that provenance. This change is locally validated, not merged or deployed. Next measurement: native existing-agent output opening at the same fixture sizes, including IPC and rendering, before assigning an interaction-level speedup.
