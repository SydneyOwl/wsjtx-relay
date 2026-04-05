# Relay

This directory merges `wsjtx-relay-client` and `wsjtx-relay-server` into one Go module inside `WsjtxWatcher`.

## Layout

- `cmd/wsjtx-relay-client`: WSJT-X/JTDX ingest client entrypoint
- `cmd/wsjtx-relay-server`: relay server entrypoint
- `internal/client`: client-specific config, relay logic, and TOFU trust store
- `internal/server`: server-specific config, runtime, and TLS helpers
- `internal/shared`: shared auth proof and websocket envelope helpers
- `configs`: example YAML configs copied from the original projects

## Notes

- The module path is `github.com/sydneyowl/wsjtxwatcher/relay`.
- The shared proto dependency still points to the sibling `wsjtx-relay-proto` repository via `go.mod`.
- The original `wsjtx-relay-client` and `wsjtx-relay-server` folders were not modified.

## Commands

```powershell
go test ./...
go run ./cmd/wsjtx-relay-server --config ./configs/server.example.yaml
go run ./cmd/wsjtx-relay-client --config ./configs/client.example.yaml
```
