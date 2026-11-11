.PHONY: verify build test e2e ui-build check-design check-contract

verify:
	./scripts/verify.sh

check-contract:
	node scripts/check-api-contract.mjs

check-design:
	cd ui && node scripts/check-design.mjs

ui-build:
	cd ui && ./node_modules/.bin/tsc --noEmit && node scripts/check-design.mjs && ./node_modules/.bin/vite build

test:
	go test -race -count=1 ./internal/... ./cmd/...

e2e: ui-build
	go test -race -count=1 -v ./e2e/...

build: ui-build
	go build -o bin/golab ./cmd/server
