# Session snapshot: remove escalation-history N+1 reads

## Journey and boundaries

Assumed priority (no usage telemetry): reopening an established session from the
sidebar is frequent and should not repeatedly read escalation data. This change
measures **only the backend boundary `App.GetSession` entry to complete returned
`SessionSnapshot`**. Wails transport, JSON serialization, React rendering,
provider response time and full native interaction latency are not measured.
No UI, provider, database schema, network or dependency changes are involved.

Invariants: preserve chat IDs/order/content, attachment metadata, answered
questions in history, only open escalations in the badge list, session isolation,
missing/malformed-reference handling and fresh answers on subsequent reads.
Live events continue to resolve the current escalation directly from SQLite.

## Existing-tooling preflight and bottleneck

The existing Go benchmark/pprof framework and SQLite store were sufficient; no
new profiler or caching library was needed. Before baseline collection, tests
passed for `./app`, `./notes`, and `./cmd/fragile` on upstream main `27094f3374592e044b6b9e0f534709fd6b1e63e8`.

`GetSession` already retrieves all session escalations, but its chat mapping
then called `FindEscalation` separately for every escalation history row. A
baseline CPU profile attributed 1.33 s of 3.19 s sampled CPU to that lookup and
2.25 s to `GetSession`. Profiles include benchmark setup/teardown even though
benchmark timing excludes them, so these are diagnostic samples, not exact
request CPU shares. The read/lock overhead, rather than a hypothetical render
bottleneck, motivated the small change.

Snapshot mapping now reuses an invocation-local ID map from the already-loaded
session-scoped list. Nothing is cached across calls. An ID absent from this list falls back to the
existing store lookup: an escalation may be created between the list read and
the later event-history read. The existing session-access check still applies. By source inspection (not
an instrumented query counter), this removes one SQL query per escalation
history row present in that list; miss-only fallback retains concurrent visibility; seven fixed queries remain for these one-orchestrator fixtures.

## Controlled measurements

Local lab, 2026-10-08: Apple M4, macOS Darwin 27, ARM64, Go 1.26.0;
`GOMAXPROCS=10` (host default). Synthetic temporary file-backed SQLite databases,
WAL mode, one connection, no network or provider calls. Each row has a fixed
240-character assistant text payload, except every fifth row in escalation fixtures.
Escalations alternate answered/open. Fresh fixture per benchmark calibration,
then one explicitly excluded complete-snapshot warmup. OS/SQLite warm-cache
measurements; not cold-start measurements or real-user monitoring.

The identical benchmark harness was compiled into separate baseline and
candidate binaries. Twelve pairs ran sequentially, alternating which binary
ran first, with Go's adaptive iteration count and `-test.benchtime 200ms`.
Results below summarize per-run mean `ns/op` using median and min–max over the
12 runs. They are **not individual-request p95 percentiles**. Nanosecond units
are Go's output units, not a claim of nanosecond measurement precision. An authenticated E2E process was active on the same host during final
collection, so host background work is not fully isolated; alternating pairs
control drift and reported ranges retain all samples. Absolute sub-millisecond values are local-lab results only.

| Synthetic fixture | Baseline median (range), ms | Candidate median (range), ms | Median change |
| --- | --- | --- | --- |
| 200 rows, no escalations | 0.458 (0.455–0.468) | 0.459 (0.443–0.489) | Within noise; no speedup claim |
| 200 rows, 40 escalations | 0.742 (0.733–0.784) | 0.460 (0.447–0.477) | −0.282 ms / −38.0% |
| 1,000 rows, 200 escalations | 3.357 (3.337–3.607) | 1.929 (1.890–2.032) | −1.428 ms / −42.5% |

For 1,000 rows, allocations fell from 24,810 to 17,813 per call (−28.2%), and
allocated bytes from approximately 2,150,601 to 1,933,110 (−10.1%). Ordinary
sessions retained 3,243 allocations per call. No timing ceiling is imposed on
CI: noisy elapsed timings would make a brittle gate. Keep the benchmark for
controlled regression investigation and the correctness tests in the fast suite.

Exact comparison identities (SHA-256):

- Baseline production source: upstream main `27094f3374592e044b6b9e0f534709fd6b1e63e8`.
- Candidate `app/sessions.go`: `8d29796341f5dcf5efbb02b0e8f3b7a48da8fec900442dff2360c4347de30dde`.
- Identical `app/session_benchmark_test.go`: `cf56821e5acf0936a36227c49ac5e4f9bfb617316b0dedd2bb567f24d2a660e1`.
- Baseline benchmark binary: `7da03add5e145b20a822d376bb41738743cc90f2e5996a9fb897de76ccaf5b20`.
- Candidate benchmark binary: `6a85c2a32e090c9d58ad3f53960026c24fc47c5c1d81bffb73a766a2c084f564`.

## Reproduce and protect correctness

Use the same Go version and host for both versions, copying the unchanged
benchmark file into a separate clean worktree at the baseline commit. Compile
each version separately, then alternate binary invocations:

```sh
go test -c -o /tmp/fragile-baseline.test ./app
# In the candidate checkout:
go test -c -o /tmp/fragile-candidate.test ./app
# Repeat >=10 times per binary, alternating baseline/candidate order:
/tmp/fragile-baseline.test -test.run '^$' -test.bench '^BenchmarkSessionSnapshot$' -test.benchtime 200ms -test.benchmem
/tmp/fragile-candidate.test -test.run '^$' -test.bench '^BenchmarkSessionSnapshot$' -test.benchtime 200ms -test.benchmem
# Diagnostic profiles, separate from timing comparisons:
/tmp/fragile-candidate.test -test.run '^$' -test.bench '^BenchmarkSessionSnapshot/escalations_1000$' -test.benchtime 2s -test.cpuprofile /tmp/fragile-candidate.cpu
go tool pprof /tmp/fragile-candidate.test /tmp/fragile-candidate.cpu

go test -race ./app ./notes ./cmd/fragile
go vet ./app ./notes ./cmd/fragile
cd app/frontend && npm ci && npm run build
```

Added targeted coverage checks open/answered/repeated escalation rows,
missing/foreign/malformed references, independent row pointers, subsequent
answer freshness, and an old snapshot remaining unchanged. Existing tests
exercise live MCP escalation answers and failed delivery reopening.

Local final-source checks passed: Go race suite (`app`, `notes`, `cmd/fragile`),
Go vet, diff checks and frontend production build. Installed-Codex sandbox smoke
also passed. The separately opted-in authenticated backend E2E timed out before
worker/MCP activity because Codex reported it was not signed in with ChatGPT;
this is a disclosed environment blocker, not a passing authenticated E2E.

Candidate re-profiling removes `FindEscalation` from the measured map-hit snapshot path. The
remaining work is event loading/mapping and SQLite read locking; no second
optimization is included without another controlled experiment. This bounded
win does not establish that escalation-heavy sessions represent typical user
traffic or that native rendering is faster. Native UI timing and field latency
remain unverified; local/reviewed/merged/deployed states must be tracked separately.
