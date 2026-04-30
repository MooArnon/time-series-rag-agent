# Trading Strategy

## Overview

This bot trades Binance Futures perpetuals on the 15-minute timeframe using a RAG (Retrieval-Augmented Generation) pipeline. On each closed candle the system scans multiple symbols, scores them with a composite technical filter, and routes only the best opportunity to an LLM for a final LONG / SHORT / HOLD decision.

---

## Watched Symbols

| Symbol   | Market          |
|----------|-----------------|
| BTCUSDT  | Bitcoin         |
| ETHUSDT  | Ethereum        |
| SOLUSDT  | Solana          |
| XRPUSDT  | XRP             |
| BNBUSDT  | Binance Coin    |

All symbols share the same 15m interval. A single BTC book-ticker WebSocket drives the timing heartbeat; all five symbols fire on the same interval boundary.

---

## Per-Bar Execution Flow

```
15m candle closes
    │
    ▼
[PARALLEL — one goroutine per symbol]
FetchLatestCandles(symbol, 15m, 130 bars)
    ↓
RunPrefilter → composite score (0–100)
    │
    ▼
SelectBestOpportunity
  • Collect all 5 scores
  • Pick the highest score that meets the threshold (default 35)
  • If none qualify → HOLD all, exit
    │
    ▼
NewLivePipeline(winner_symbol)
  ├─ Position check       → skip if position already open
  ├─ Cooldown check       → skip if SL cooldown active
  ├─ Daily ROI gates      → stop-loss / stop-profit circuit breaker
  ├─ PrefilterGate        → re-runs on the winner (cheap, confirms score)
  ├─ NewLLMPatternAgent   → pgvector pattern retrieval + Claude signal
  └─ NewOrderExecutionPipeline → place or skip trade
```

---

## Prefilter Scoring (0–100)

The prefilter evaluates each symbol independently on the same 130-candle window. Only the highest scorer proceeds to the LLM.

| Component        | Max Points | Logic |
|------------------|-----------|-------|
| S/R Proximity    | +20       | Distance to nearest swing pivot (50-bar lookback) |
| Candle Body      | +15       | Body size / ATR — strong body = conviction |
| Volume Surge     | +20       | Current volume vs 20-bar average (bonus if > 1×) |
| MA Alignment     | +15       | MA7 / MA25 / MA99 cleanly fanned in one direction |
| ADX Bonus        | +12       | +12 if ADX > 40; +10 if ADX > 25 |
| Chop Penalty     | −15       | Tight 10-bar range relative to 2×ATR |
| Stale Penalty    | −10       | Last 5 candles all small-bodied (< 0.3×ATR) |

**Threshold**: configurable via `PREFILTER_THRESHOLD` env var (default 35).

---

## Selection Rule

> Among all symbols that score at or above the threshold, the one with the **highest absolute score** wins and enters the full pipeline. All others are silently skipped for that bar.

This concentrates LLM calls and trade attempts on the single clearest setup available at any moment, regardless of which symbol it is.

---

## Risk Controls (applied to the winner)

| Control            | Condition                          | Action     |
|--------------------|------------------------------------|------------|
| Open position      | Any futures position already open  | HOLD all   |
| SL cooldown        | Recent stop-loss within N bars     | HOLD all   |
| Daily stop-loss    | ROI ≤ `STOP_LOSS_ROI`             | HOLD all   |
| Daily take-profit  | ROI ≥ `STOP_ROI`                  | HOLD all   |
| LLM confidence     | Confidence < `CONFIDENCE_THRESHOLD`| HOLD       |

One open position at a time. Multi-symbol scanning does not enable concurrent positions.
