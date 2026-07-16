# Northrou monorepo Makefile
# The backend and coordination server are separate Go modules.

VERSION ?= dev
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X github.com/rhymeswithlimo/northrou/backend/internal/buildinfo.Version=$(VERSION) \
           -X github.com/rhymeswithlimo/northrou/backend/internal/buildinfo.Commit=$(COMMIT) \
           -X github.com/rhymeswithlimo/northrou/backend/internal/buildinfo.Date=$(DATE)

WEB_ASSETS := backend/internal/web/assets

.PHONY: build build-backend build-coord build-relay frontend frontend-dev test vet tidy clean clean-frontend run

# The backend embeds the client, so the client has to exist before it compiles.
build: frontend build-backend build-coord build-relay

# Build the Vite client and stage it where //go:embed can see it.
# The output is generated, not committed: everything under $(WEB_ASSETS) except
# .gitkeep is ignored, and .gitkeep is what keeps `go build` working in a fresh
# clone that has not run this yet.
frontend:
	cd frontend && npm ci --silent && npm run build
	find $(WEB_ASSETS) -mindepth 1 ! -name .gitkeep -delete
	cp -R frontend/dist/. $(WEB_ASSETS)/

# Live-reloading client on :5173, proxying /api to a locally running server.
frontend-dev:
	cd frontend && npm install --silent && npm run dev

build-backend:
	cd backend && go build -ldflags "$(LDFLAGS)" -o ../bin/northrou ./cmd/northrou

build-coord:
	cd coordination && go build -o ../bin/coordinator ./cmd/coordinator

build-relay:
	cd coordination && go build -o ../bin/relay ./cmd/relay

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

clean-frontend:
	rm -rf frontend/dist frontend/node_modules
	find $(WEB_ASSETS) -mindepth 1 ! -name .gitkeep -delete

clean:
	rm -rf bin dist
