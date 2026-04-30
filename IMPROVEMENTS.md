# Project Improvement Plan — time-series-rag-agent

## System Flow (Current State)

```
WebSocket (Binance)
        │  closed candle
        ▼
  StartKlineWebsocket  [cmd/live/main.go]
        │  atomic guard (drops bar if pipeline running)
        ▼
  NewLivePipeline  [pipeline/live_flow.go]
   ├─ [parallel] FetchLatestCandles (REST)
   ├─ [parallel] NewPostgresDB
   └─ [parallel] GetCooldownState
        │
        ▼
  NewEmbeddingPipeline
   ├─ MergeCandles (WS + REST)
   ├─ FeatureCalculator  → log-return + z-score vector
   └─ LabelCalculator    → historical outcome labels
        │
        ▼
  [parallel] UpsertFeature + UpsertLabels + RestIngestVectorFlow(1h)
        │
        ├─ HasOpenPosition? → skip LLM if position exists
        ├─ IsInCooldown?    → HOLD if SL cooldown active
        ├─ DailyROI check  → stop-loss / stop-profit gate
        └─ PrefilterGate   → composite score (ATR, S/R, body, volume, MA, ADX)
                │
                ▼ (only if score ≥ threshold)
        NewLLMPatternAgent  [pipeline/llm_agent_flow.go]
         ├─ QueryTopN(15m)  — pgvector similarity search
         ├─ QueryTopN(1h)   — multi-timeframe context
         ├─ GenerateCandleChart → candle.png
         ├─ FetchLatestRegimes(4h, 1d)
         ├─ CalculateDailyROI
         ├─ GetPositionHistory
         └─ GenerateSignal (OpenRouter LLM)
                │
                ▼
        Confidence check
                │
                ▼
        NewOrderExecutionPipeline
         ├─ CalculateDailyROI (again)
         ├─ SetLeverage
         └─ PlaceTrade / CancelTrade
                │
                ▼
        Discord notification
        PostgreSQL trade signal log (fire-and-forget goroutine)
```

---

## Bugs

### B1 — 1h ingest runs every bar (not only at :00)
**File:** `pipeline/live_flow.go:112`
`RestIngestVectorFlow` for the `1h` timeframe is called on every 15m bar. It should only run when the clock crosses a new 1h boundary.
```go
// TODO comment in code already acknowledges this
g2.Go(func() error {
    if err := RestIngestVectorFlow(logger, symbol, "1h", vectorSize); err != nil { ...
```
**Fix:** Add a time check — only run when `time.Now().UTC().Minute() == 0`.

### B2 — Double `CalculateDailyROI` per cycle
**Files:** `pipeline/live_flow.go:142` and `pipeline/order_execution_flow.go:19`
ROI is fetched twice in the same pipeline run, making two redundant Binance API calls.
**Fix:** Pass the already-fetched `roi` value down to `NewOrderExecutionPipeline` as a parameter.

### B3 — LLM agent opens a second DB connection
**File:** `pipeline/llm_agent_flow.go:26`
`NewLLMPatternAgent` opens its own `postgresql.NewPostgresDB` connection even though `live_flow.go` already has one open. Two connections are wasted per cycle.
**Fix:** Inject the existing `*postgresql.PatternStore` into `NewLLMPatternAgent`.

### B4 — `RestIngestVectorFlow` ignores parent context
**File:** `pipeline/db_ingest_flow.go:25`
Uses `context.Background()` instead of the `ctx` passed by the caller, so cancellation (e.g. SIGTERM) is ignored.
**Fix:** Accept and thread `ctx` through properly.

### B5 — `candle.png` write is not concurrency-safe
**File:** `pipeline/llm_agent_flow.go:52`
`GenerateCandleChart` writes a fixed filename `candle.png` to the working directory. If two pipeline runs somehow overlap (e.g. during a restart race), the file is overwritten mid-read.
**Fix:** Write to a temp file with a unique name (e.g. `candle-<unix>.png`) and clean up after upload.

### B6 — `config.LoadConfig()` called multiple times per cycle
Multiple pipeline stages call `config.LoadConfig()` independently (`live_flow.go`, `order_execution_flow.go`, `db_ingest_flow.go`). Each call re-reads and parses the YAML.
**Fix:** Load once in `main.go` and pass `*config.AppConfig` through the pipeline.

---

## Architecture Improvements

### A1 — Hardcoded symbol/interval/vector size in `main.go`
```go
const (
    SYMBOL      = "BTCUSDT"
    INTERVAL    = "15m"
    VECTOR_SIZE = 30
)
```
Multi-symbol or multi-interval support requires code changes. Move these to `config.yaml` under an `agent.symbols` list, and loop over them in `main`.

### A2 — Multiple timeframe pipeline wiring
The `feat-multiple-timeframe` branch goal: run independent pipelines for different intervals and feed all their embeddings into the same LLM prompt. Current design calls `RestIngestVectorFlow(1h)` inline as a hack. Proper design:
- `SymbolIntervalPipeline` struct wrapping (symbol, interval, vectorSize)
- One goroutine per (symbol, interval) pair
- Aggregation step merges signals before LLM call

### A3 — DB connection pool per cycle is expensive
A new `pgx` connection is opened and closed on every 15m bar. Use a long-lived connection pool (e.g. `pgxpool`) initialized once at startup and shared across pipeline runs.

### A4 — No retry logic on transient errors
Binance REST calls, DB upserts, and LLM calls all fail hard on the first error. Add exponential backoff with jitter for:
- `FetchLatestCandles`
- `GenerateSignal` (LLM)
- `InsertTradeSignal`

### A5 — Signal log insert is fire-and-forget with no visibility
```go
go func() { ... dbIngest.InsertTradeSignal(...) }()
```
Errors are logged but silently dropped. Use a buffered channel or a small worker pool so inserts can be retried without blocking the order path.

### A6 — No metrics / observability
There are no Prometheus metrics, no structured latency tracking, and no alerting on pipeline failures beyond Discord. Add:
- Per-stage latency histograms (embedding, LLM, order)
- Counters: signals fired, orders placed, prefilter skip rate
- Gauge: current cooldown bars remaining

---

## Feature Improvements

### F1 — Prefilter score persistence
The prefilter score is computed but never stored. Log it to `trade_signal_log` so you can correlate score ranges with realized P&L and tune the threshold empirically.

### F2 — Regime stored per cycle
`FetchLatestRegimes` result (TRENDING / RANGING / VOLATILE + direction) is passed to the LLM prompt but never persisted. Store it alongside the signal log for backtesting.

### F3 — Cooldown state is in-memory only
`cooldown.Manager` state is lost on restart. A stopped bot resumes as if no recent SL occurred. Persist `consecutiveLosses` and `resumeAfter` to PostgreSQL or Redis.

### F4 — LLM token budget has no feedback loop
`MaxDailyTokens` is a hard cap but there is no alerting when the budget is close to exhaustion. Add a Discord warning at 80% consumption.

### F5 — Pattern quality degrades without label refresh
Labels in the DB (outcome of each historical pattern) are written at backfill time and updated on each live bar. Patterns older than N days may have stale labels. Add a periodic job to recompute labels for the rolling window.

### F6 — No backtesting harness
There is a `cmd/backfill` that builds the DB, but no way to replay signals against historical bars offline to evaluate strategy P&L before going live with config changes.

---

## Code Quality

### Q1 — `regime.go` duplicates ADX logic from `prefilter/gate.go`
`CalcADX` in `exchange/regime.go` and `ComputeADX` in `prefilter/gate.go` implement the same algorithm on different types (`RestCandle` vs `WsRestCandle`). Extract a shared `indicators` package.

### Q2 — Empty `config.yaml` and `DEVELOP.md`
Both files exist but are effectively empty. Populate `config.yaml` with documented defaults and `DEVELOP.md` with local setup instructions.

### Q3 — `plot/candel.go` typo in filename
File is named `candel.go` (typo for `candle.go`). Rename for consistency.

### Q4 — Test coverage gaps
- `pipeline/` has no tests
- `exchange/order_executor.go` has no unit tests
- `prefilter/gate.go` has tests but no table-driven edge cases for the ADX bonus and stale penalty branches
Add integration tests for the embedding → DB upsert → query round-trip.

---

## Prioritized TODO List

### P0 — Critical (fix before next live run)

- [ ] **B1** Fix 1h ingest — add `time.Now().UTC().Minute() == 0` guard in `live_flow.go:112`
- [ ] **B2** Eliminate double `CalculateDailyROI` — pass `roi` from `live_flow` to `NewOrderExecutionPipeline`
- [ ] **B4** Thread `ctx` through `RestIngestVectorFlow` — replace `context.Background()` with the caller's ctx
- [ ] **B5** Fix `candle.png` race — write to a temp unique path

### P1 — High (this sprint)

- [ ] **B3** Inject DB connection into `NewLLMPatternAgent` — remove the second `NewPostgresDB` call
- [ ] **B6** Load config once in `main`, pass `*config.AppConfig` everywhere
- [ ] **A3** Replace per-cycle DB open/close with `pgxpool` initialized at startup
- [ ] **F3** Persist cooldown state to DB so restarts don't reset it
- [ ] **A1** Move SYMBOL/INTERVAL/VECTOR_SIZE to `config.yaml`; load as a list for future multi-symbol support

### P2 — Medium (next sprint)

- [ ] **A2** Refactor `RestIngestVectorFlow(1h)` into the proper multi-timeframe pipeline structure
- [ ] **A4** Add retry with exponential backoff for Binance REST and LLM calls
- [ ] **F1** Persist prefilter score in `trade_signal_log`
- [ ] **F2** Persist regime result in `trade_signal_log`
- [ ] **Q1** Extract shared `indicators` package (ADX, ATR, BBW) — remove duplication between `regime.go` and `prefilter/gate.go`
- [ ] **Q4** Add pipeline integration tests (embedding → upsert → QueryTopN round-trip)

### P3 — Low (backlog)

- [ ] **A5** Replace fire-and-forget signal log goroutine with a buffered retry channel
- [ ] **A6** Add Prometheus metrics endpoint (latency, signal counters, prefilter skip rate)
- [ ] **F4** Discord warning at 80% LLM daily token budget
- [ ] **F5** Periodic label refresh job for patterns older than 30 days
- [ ] **F6** Offline backtesting harness using the backfill DB
- [ ] **Q2** Populate `config.yaml` with all defaults and document each field
- [ ] **Q2** Fill in `DEVELOP.md` with local setup steps (docker-compose, env, backfill, live run)
- [ ] **Q3** Rename `plot/candel.go` → `plot/candle.go`
