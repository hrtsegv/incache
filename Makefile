.PHONY: fmt vet build test test-race bench lint vulncheck ci

fmt:
	gofmt -l .

vet:
	go vet ./...

build:
	go build ./...

test:
	go test ./... -cover

test-race:
	go test ./... -race -cover

bench:
	go test -bench=. -run=^$$ -benchmem ./...

lint:
	golangci-lint run

vulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

ci: fmt vet build test-race lint vulncheck
