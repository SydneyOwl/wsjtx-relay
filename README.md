# wsjtx-relay

`wsjtx-relay` is a small relay stack for forwarding live `WSJT-X` / `JTDX` traffic to remote watchers.

It is useful when:

- your phone is not on the same LAN as the radio computer
- you want to expose one station to multiple remote viewers
- you want `WsjtxWatcher` to consume a remote source instead of direct UDP

The project contains both sides of the relay:

- `wsjtx-relay-server`
  - public or private relay service
  - accepts authenticated `ingest` and `watch` WebSocket sessions
- `wsjtx-relay-client`
  - local bridge that listens to WSJT-X UDP
  - pushes decoded events to the relay server

## What It Does

The relay keeps the existing WSJT-X UDP workflow on the station machine, then adds a secure live bridge:

1. `wsjtx-relay-client` listens to WSJT-X / JTDX UDP locally.
2. The client converts live WSJT-X events into protobuf relay frames.
3. The client sends those frames to `wsjtx-relay-server` over `wss://`.
4. `WsjtxWatcher` connects to the server as a `watch` client.
5. The watcher selects a source and receives live decode, status, session, and QSO events.

The relay is live-state oriented. It does not provide historical decode replay.

## Repository Layout

- `cmd/wsjtx-relay-client`
  - WSJT-X / JTDX ingest client entrypoint
- `cmd/wsjtx-relay-server`
  - relay server entrypoint
- `internal/client`
  - client config, relay connection logic, and TOFU trust store
- `internal/server`
  - server config, runtime, fan-out, and TLS helpers
- `internal/shared`
  - shared auth proof and WebSocket envelope helpers
- `configs`
  - example YAML configuration files

## Security Model

- Transport uses WebSocket over TLS.
- The server can auto-generate and persist a self-signed certificate.
- Clients use TOFU trust by storing the server SPKI fingerprint on first successful connection.
- Authentication uses a shared secret plus a time-bounded HMAC proof.
- Tenant separation is based on a high-entropy shared `tenant_id` plus the shared secret.
- The current design does not maintain a separate server-side secret per tenant.
- Relay frame size is currently limited to `1 MiB`.

Important security notes:

- `tenant_id` should be treated as a private high-entropy identifier, not a friendly room name.
- `public: true` disables authentication entirely and should only be used on trusted networks or for testing.

## Server Configuration

The example server config is in `configs/server.example.yaml`:

```yaml
listen_addr: "0.0.0.0:8443"
data_dir: ./data
shared_secret: "replace-with-a-strong-secret"
heartbeat_interval: 10s
heartbeat_timeout: 30s
max_timestamp_skew: 90s
```

Important server settings:

- `listen_addr`
  - HTTPS / WebSocket listen address
- `data_dir`
  - storage location for generated TLS files
- `shared_secret`
  - shared secret for relay authentication (required unless `public: true`)
- `public`
  - set to `true` to disable authentication (open relay mode)
- `heartbeat_interval`
  - ping interval sent to connected clients
- `heartbeat_timeout`
  - idle timeout before a session is closed
- `max_timestamp_skew`
  - allowed clock skew for auth requests

Optional server settings supported by the binary:

- `cert_file`
  - existing TLS certificate file
- `key_file`
  - existing TLS private key file

Behavior notes:

- If no certificate is configured, the server generates one and logs its SPKI fingerprint.
- When `public: true`, `shared_secret` is not required and authentication is skipped.
- `public: true` means anyone who can reach the server can connect as either `ingest` or `watch`.

## Client Configuration

The example client config is in `configs/client.example.yaml`:

```yaml
udp_listen_addr: ":2237"
server_url: "wss://example.com:8443"
shared_secret: "replace-me"
tenant_id: "replace-with-a-random-shared-id"
source_name: "station-a"
```

### Minimal required client settings

For a basic working setup, you only need:

```yaml
udp_listen_addr: ":2237"
server_url: "wss://example.com:8443"
shared_secret: "replace-me"
tenant_id: "replace-with-a-random-shared-id"
source_name: "station-a"
```

You can omit:

- `data_dir`
  - if omitted, the client uses `./data`
- `trust_store_path`
  - if omitted, the client stores the trusted fingerprint under `data_dir/trusted_server_fingerprint.txt`
- `auto_trust_on_first_use`
  - if omitted, it defaults to `true`
- `client_name`
  - if omitted, it defaults to `wsjtx-relay-client`
- `client_version`
  - if omitted, it defaults to the build version
- `instance_id`
  - if omitted, the client generates a fresh random instance ID on startup

### Client setting details

- `udp_listen_addr`
  - local UDP address used to receive WSJT-X / JTDX traffic
- `server_url`
  - relay server base URL
  - should be `wss://host:port`
- `shared_secret`
  - must match the server-side shared secret
- `tenant_id`
  - a shared private high-entropy tenant identifier that both the relay client and the watcher must use
  - it participates in authentication and tenant routing, so treat it as secret configuration
  - use a long random value, not a human-friendly name like `home`, `test`, or `station1`
- `source_name`
  - logical source identifier inside the tenant
- `source_display_name`
  - user-facing display label shown in watchers
- `trust_store_path`
  - file that stores the trusted server fingerprint
- `auto_trust_on_first_use`
  - if `true`, the first seen server fingerprint is trusted and saved automatically
- `instance_id`
  - optional stable client instance identifier
  - useful if you want a process restart to replace the previous session cleanly

## WsjtxWatcher Configuration

To use this relay with `WsjtxWatcher`, open the app settings and select `Third-party data source`, then configure:

- `Server URL`
  - the relay server watch endpoint base URL, for example `wss://example.com:8443`
- `Shared Secret`
  - same secret used by the relay client and server
- `Tenant ID`
  - the same shared private high-entropy ID used by the relay client
  - this identifies the tenant namespace and also participates in authentication
- `Select source`
  - choose which relay source the app should watch
- `Refresh source list`
  - reconnect and refresh available sources from the relay server
- `Re-pair server`
  - clears the stored TOFU trust so the app can pair with a new certificate

On the first successful connection, `WsjtxWatcher` stores the observed server fingerprint automatically.

## Quick Start

### 1. Start the relay server

```powershell
go run ./cmd/wsjtx-relay-server --config ./configs/server.example.yaml
```

Make sure to set a strong `shared_secret` in the config file. Use `--public` if you want to skip authentication.

Do not expose `public: true` on the open internet unless you intentionally want an unauthenticated open relay.

### 2. Configure and start the relay client on the station machine

- Point WSJT-X or JTDX UDP output to the address in `udp_listen_addr`.
- Set `server_url` to the relay server.
- Copy the same `shared_secret`.
- Choose a long random `tenant_id`.
  - A simple way is to generate a random hex, base32, or base64url string and use the same value on both sides.
  - Treat it as private configuration, not as a display name.
- Set a unique `source_name`.

Recommended operational notes:

- Keep `shared_secret` and `tenant_id` out of screenshots, issue trackers, and chat logs.
- If either value is exposed, rotate both values for that tenant setup.
- Prefer running behind a firewall, Tailscale, WireGuard, or a reverse proxy with connection controls if you expose the service remotely.

Then start the client:

```powershell
go run ./cmd/wsjtx-relay-client --config ./configs/client.example.yaml
```

### 3. Configure `WsjtxWatcher`

- Open `Settings`.
- Change `Data source` to `Third-party data source`.
- Fill in:
  - `Server URL`
  - `Shared Secret`
  - `Tenant ID` (same in step 2)
- Save settings.
- Start the watcher service from the main screen.
- Open `Select source` and choose the desired relay source.

## Commands

```powershell
go test ./...
go run ./cmd/wsjtx-relay-server --config ./configs/server.example.yaml
go run ./cmd/wsjtx-relay-client --config ./configs/client.example.yaml
```

## Notes

- Module path: `github.com/sydneyowl/wsjtx-relay`
- The generated protobuf dependency is pulled from the sibling `wsjtx-relay-proto` repository.
- The relay is designed for live event forwarding, not backlog playback.
