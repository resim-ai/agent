# Clean up Go environment and delete built local binaries
@clean:
 go clean

# Run all Go tests
@test:
 go test . -v -race

# Run all Go tests using grc to colour the output for easier reading
@test_colour:
 grc go test . -v -race

 # Run integration tests - assumes envirnment is set - see READEM in test/integration for example
@integration_test:
  go test ./test/integration -v

# Generate Go coverage report
@test_coverage:
 go test . -coverprofile=coverage.out

# Run go generate for project
@generate:
  go generate ./...

# Fetch Go dependencies
@dep:
 go mod download

# Run go vet
@vet:
 go vet

# Local linting - requires `golangci-lint` to be installed
@lint:
 golangci-lint run
