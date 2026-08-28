.PHONY: run test

run:
	air

test:
	sqlc generate
	@echo "lint: checking gofmt"
	@files="$$(rg --files -g '*.go')"; formatted="$$(gofmt -l $$files)"; if [ -n "$$formatted" ]; then echo "unformatted Go files:" >&2; echo "$$formatted" >&2; exit 1; fi
	go vet ./...
	go test -count=1 ./...
