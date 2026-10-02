# Go packages only (./... would descend into web/node_modules).
PKGS    := ./cmd/... ./internal/... ./web
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build web test check dev-api dev-web docker clean

build: web                ## single binary with the UI embedded
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o geotracker ./cmd/geotracker

web:
	cd web && npm ci --no-audit --no-fund && npm run build

test:                     ## backend tests (no UI build needed)
	go test ./internal/...

check: web                ## everything CI runs
	go vet $(PKGS)
	go test ./internal/...
	cd web && npm run typecheck

dev-api:                  ## API on :8080 (needs web/dist once: run `make web`)
	go run ./cmd/geotracker

dev-web:                  ## UI with hot reload on :5173, proxying to :8080
	cd web && npm run dev

docker:
	docker build --build-arg VERSION=$(VERSION) -t geotracker:$(VERSION) .

clean:
	rm -rf geotracker geotracker.exe web/dist
