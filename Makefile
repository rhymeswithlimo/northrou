# Northrou monorepo Makefile
# The backend and coordination server are separate Go modules.

VERSION ?= dev
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X github.com/rhymeswithlimo/northrou/backend/internal/buildinfo.Version=$(VERSION) \
           -X github.com/rhymeswithlimo/northrou/backend/internal/buildinfo.Commit=$(COMMIT) \
           -X github.com/rhymeswithlimo/northrou/backend/internal/buildinfo.Date=$(DATE)

.PHONY: build build-backend build-coord test vet tidy clean run

build: build-backend build-coord

build-backend:
	cd backend && go build -ldflags "$(LDFLAGS)" -o ../bin/northrou ./cmd/northrou

build-coord:
	cd coordination && go build -o ../bin/coordinator ./cmd/coordinator

test:
	cd backend && go test ./...
	cd coordination && go test ./...

vet:
	cd backend && go vet ./...
	cd coordination && go vet ./...

tidy:
	cd backend && go mod tidy
	cd coordination && go mod tidy

run: build-backend
	./bin/northrou serve -v

clean:
	rm -rf bin dist
