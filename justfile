set dotenv-load := true

run:
    air

mkhandler name:
    go run ./cmd/mkhandler {{name}}

check:
    sqlc generate
    swag init -q -g cmd/backend/main.go -d ./ -o docs --parseInternal --parseGoList=false --outputTypes json,yaml
    gofmt -w .
    go build ./...
    go vet -tags integration ./...
    go test -count=1 ./...

# Slow process-level tests in tests/ (shutdown, Litestream); not part of check or CI.
test-integration:
    go test -tags integration -count=1 ./tests/
