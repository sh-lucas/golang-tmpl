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
    go vet ./...
    go test -count=1 ./...
