#!/bin/bash
set -e

echo "=== Backfill: 1h embeddings ==="

go run cmd/backfill/main.go -symbol BTCUSDT -interval 1h -days 1000
go run cmd/backfill/main.go -symbol ETHUSDT -interval 1h -days 1000
go run cmd/backfill/main.go -symbol SOLUSDT -interval 1h -days 1000
go run cmd/backfill/main.go -symbol XRPUSDT -interval 1h -days 1000
go run cmd/backfill/main.go -symbol BNBUSDT -interval 1h -days 1000

echo "=== Backfill: 15m embeddings ==="

go run cmd/backfill/main.go -symbol BTCUSDT -interval 15m -days 1000
go run cmd/backfill/main.go -symbol ETHUSDT -interval 15m -days 1000
go run cmd/backfill/main.go -symbol SOLUSDT -interval 15m -days 1000
go run cmd/backfill/main.go -symbol XRPUSDT -interval 15m -days 1000
go run cmd/backfill/main.go -symbol BNBUSDT -interval 15m -days 1000

echo "=== Done ==="
