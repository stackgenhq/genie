.PHONY: test test-analyzer test-iacgen test-all test-verbose test-coverage

# Run all tests
test:
	go test -mod=mod ./...
