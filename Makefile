.PHONY: run check

run:
	air

check:
	sqlc generate
	swag init -q -g main.go -d cmd/backend,internal/features/admins,queries -o docs --parseInternal --parseGoList=false --outputTypes json,yaml
	gofmt -w $$(rg --files -g '*.go')
	go vet ./...
	go test -count=1 ./...
