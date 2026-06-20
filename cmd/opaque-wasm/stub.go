//go:build !js || !wasm

// This stub keeps `go build ./...` / `go vet ./...` happy on non-WASM hosts,
// where main.go (//go:build js && wasm) is excluded. The real entrypoint is
// built only for GOOS=js GOARCH=wasm.
package main

func main() {}
