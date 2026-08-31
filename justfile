set dotenv-load := true

run:
    air

mkhandler name:
    go run ./cmd/mkhandler {{name}}

check:
    sqlc generate
    swag init -q -g main.go -d cmd/backend,internal/features/admins,internal/features/libsql,internal/features/health,queries -o docs --parseInternal --parseGoList=false --outputTypes json,yaml
    gofmt -w $(rg --files -g '*.go')
    go vet ./...
    go test -count=1 ./...
