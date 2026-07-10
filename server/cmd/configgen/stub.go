//go:build !configgen

// This no-op stub keeps cmd/configgen buildable under ordinary GOWORK=off
// `go build/vet/test ./...`. The real generator (main.go) is behind the
// `configgen` build tag and imports the UNRELEASED
// framework-go-app-generator/configgen, so it compiles ONLY in workspace mode
// (`go run -tags configgen ./cmd/configgen`). See main.go's package doc for the
// release follow-up.
package main

func main() {}
