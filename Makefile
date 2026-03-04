test-unit:
	go test ./... -v

test-integration:
	go test ./internal/exchange/... -tags=integration -v