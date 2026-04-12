# time-series-rag-agent
This repository keeps all of go codes that were utilized for create the trading bot at Binance future market.

This project keeps 
1. Websocket to get the realtime data from Binance future websocket.
2. PostgreSQL logics to proceed the selection and data ingestion.
3. LLM client (OpenRouter) which keep the code to generate promt and API interaction with LLM.
4. Business logic to transform HLOC data into the analyseable infomation to feed to LLM
5. AWS services leverage which keep the code the interacwith AWS services like S3, SQS and DynamoDB.
6. Notifier service (Discord)

Flow,
1. run `go run cmd/live/live_....go` will start the listen websocket to receive data from Binance.
2. Websocker run and RESTful API run to get the latese and historical candle to combine togather.
3. Embedding candles using Log return and Z-Score.
4. Use that vector to proceed to similarity search to get the top_k to predict the next move.
5. insert the data (Go routine) while sent the top_k plot and price action (candle plot) to LLM.
extra: plot price action with MA and volume for further consideration.
6. receive result from LLM.
7. Open order or just stay still.
8. Save plot image to S3
9. Put LLM respond to SQL

```
time-series-rag-agent/
│
├── cmd/                          # Entrypoints only — ไม่มี logic อยู่ที่นี่
│   ├── live/
│   │   └── main.go               # starts websocket + trading loop
│   ├── backfill/
│   │   └── main.go               # backfill_regime + backfill_vector รวมกัน (flag-based)
│   └── worker/
│       └── main.go               # consume_que (SQS worker)
│
├── internal/                     # Private packages — core business logic
│   │
│   ├── exchange/                 # (เปลี่ยนจาก market/) — Binance-specific
│   │   ├── websocket.go          # (streamer.go) realtime candle stream
│   │   ├── rest.go               # (history.go) historical OHLCV fetch
│   │   └── model.go              # Candle, Ticker structs
│   │
│   ├── signal/                   # (เปลี่ยนจาก ai/ + market_trend/) — core analysis
│   │   ├── embedding.go          # (pattern_embedding.go) log-return + z-score vector
│   │   ├── zscore.go             # (slope_return_zscore_calculation.go)
│   │   ├── similarity.go         # vector similarity search / top-k
│   │   └── regime.go             # market regime detection
│   │
│   ├── llm/                      # LLM client (OpenRouter)
│   │   ├── client.go             # (service.go) API call
│   │   ├── prompt.go             # (format_promt.go) prompt builder
│   │   └── model.go              # request/response structs
│   │
│   ├── order/                    # Trading execution logic
│   │   └── executor.go           # open/close order decision
│   │
│   ├── plot/                     # Chart generation
│   │   ├── candle.go
│   │   └── pattern.go            # top-k pattern plots
│   │
│   ├── storage/                  # (แยก concern ชัดเจน)
│   │   ├── postgres/
│   │   │   ├── candle_repo.go
│   │   │   ├── signal_repo.go
│   │   │   └── db.go
│   │   ├── s3/
│   │   │   └── client.go         # upload plot images
│   │   └── sqs/
│   │       └── client.go         # queue producer/consumer
│   │
│   ├── notify/                   # (เปลี่ยนจาก notifier/)
│   │   └── discord.go
│   │
│   └── pipeline/                 # ★ NEW — orchestrates the full flow (1→9)
│       └── trading_pipeline.go   # wires exchange→signal→llm→order→notify
│
├── config/
│   ├── config.go                 # struct + loader
│   └── config.yaml
│
├── pkg/                          # Reusable, exportable utilities
│   ├── timeutil/
│   └── mathutil/
│
├── migrations/                   # SQL migration files
│   └── 001_init.sql
│
├── scripts/                      # Dev/ops scripts
│   └── backfill.sh
│
├── .env.example
├── docker-compose.yml
├── Makefile
└── go.mod
```