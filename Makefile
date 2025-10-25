.PHONY: api worker tidy fmt test migrate

api:   ; go run ./cmd/api
worker:; go run ./cmd/worker
tidy:  ; go mod tidy
fmt:   ; gofmt -s -w .
test:  ; go test ./... -v -count=1
migrate: ; @echo "TODO: Add migration tool (e.g., golang-migrate or goose)" && exit 1
