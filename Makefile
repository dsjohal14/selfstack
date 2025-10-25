.PHONY: api worker tidy fmt test migrate lint precommit

api:   ; go run ./cmd/api
worker:; go run ./cmd/worker
tidy:  ; go mod tidy
fmt:   ; gofmt -s -w .
test:  ; go test ./... -v -count=1
lint:  ; golangci-lint run
migrate: ; @echo "TODO: Add migration tool (e.g., golang-migrate or goose)" && exit 1

# Run all pre-commit checks
precommit: fmt tidy lint test
	@echo "✅ All pre-commit checks passed!"
