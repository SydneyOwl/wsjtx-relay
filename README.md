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
- Tenant isolation is based on `tenant_id`.
- Relay frame size is currently limited to `1 MiB`.

## Server Configuration

The example server config is in `configs/server.example.yaml`:

```yaml
listen_addr: "0.0.0.0:8443"
data_dir: ./data
heartbeat_interval: 10s
heartbeat_timeout: 30s
max_timestamp_skew: 90s
```

Important server settings:

- `listen_addr`
  - HTTPS / WebSocket listen address
- `data_dir`
  - storage location for generated TLS files and generated shared secret
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
- `shared_secret`
  - explicit shared secret value
- `shared_secret_file`
  - path to a persisted shared secret

Behavior notes:

- If no certificate is configured, the server generates one and stores it under `data_dir`.
- If no shared secret is configured, the server generates one and stores it under `data_dir/shared_secret.txt`.

## Client Configuration

The example client config is in `configs/client.example.yaml`:

```yaml
data_dir: ./data
udp_listen_addr: ":2237"
server_url: "wss://192.168.3.221:8443"
shared_secret: "replace-me"
tenant_id: "replace-with-tenant-id"
source_name: "station-a"
source_display_name: "Station A"
trust_store_path: "./data/trusted_server_fingerprint.txt"
auto_trust_on_first_use: true
client_name: "wsjtx-relay-client"
client_version: "0.1.0"
instance_id: ""
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

- `trust_store_path`
  - if omitted, the client stores the trusted fingerprint under `data_dir/trusted_server_fingerprint.txt`
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
  - a shared private ID that both the relay client and the watcher must use
  - think of it as a private room name plus secret routing key
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

## Environment Variables

Both binaries now support this precedence order:

1. built-in defaults
2. YAML config file
3. environment variables
4. CLI flags

This makes the project easier to run in Docker without mounting YAML files.

### Server environment variables

- `WSJTX_RELAY_SERVER_CONFIG`
- `WSJTX_RELAY_SERVER_LISTEN_ADDR`
- `WSJTX_RELAY_SERVER_DATA_DIR`
- `WSJTX_RELAY_SERVER_CERT_FILE`
- `WSJTX_RELAY_SERVER_KEY_FILE`
- `WSJTX_RELAY_SERVER_SHARED_SECRET`
- `WSJTX_RELAY_SERVER_SHARED_SECRET_FILE`
- `WSJTX_RELAY_SERVER_HEARTBEAT_INTERVAL`
- `WSJTX_RELAY_SERVER_HEARTBEAT_TIMEOUT`
- `WSJTX_RELAY_SERVER_MAX_TIMESTAMP_SKEW`

### Client environment variables

- `WSJTX_RELAY_CLIENT_CONFIG`
- `WSJTX_RELAY_CLIENT_DATA_DIR`
- `WSJTX_RELAY_CLIENT_UDP_LISTEN_ADDR`
- `WSJTX_RELAY_CLIENT_SERVER_URL`
- `WSJTX_RELAY_CLIENT_SHARED_SECRET`
- `WSJTX_RELAY_CLIENT_TENANT_ID`
- `WSJTX_RELAY_CLIENT_SOURCE_NAME`
- `WSJTX_RELAY_CLIENT_SOURCE_DISPLAY_NAME`
- `WSJTX_RELAY_CLIENT_TRUST_STORE_PATH`
- `WSJTX_RELAY_CLIENT_AUTO_TRUST_ON_FIRST_USE`
- `WSJTX_RELAY_CLIENT_CLIENT_NAME`
- `WSJTX_RELAY_CLIENT_CLIENT_VERSION`
- `WSJTX_RELAY_CLIENT_INSTANCE_ID`

Duration values use Go duration syntax such as `10s`, `30s`, or `2m`.
Boolean values accept standard Go forms such as `true` / `false`.

## WsjtxWatcher Configuration

To use this relay with `WsjtxWatcher`, open the app settings and select `Third-party data source`, then configure:

- `Server URL`
  - the relay server watch endpoint base URL, for example `wss://example.com:8443`
- `Shared Secret`
  - same secret used by the relay client and server
- `Tenant ID`
  - the same shared private ID used by the relay client
  - this is what tells the watcher which private relay space to join
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

If you did not set `shared_secret`, check the generated value in `data/shared_secret.txt`.

### 2. Configure and start the relay client on the station machine

- Point WSJT-X or JTDX UDP output to the address in `udp_listen_addr`.
- Set `server_url` to the relay server.
- Copy the same `shared_secret`.
- Choose a long random `tenant_id`.
  - A simple way is to generate a random hex, base32, or base64url string and use the same value on both sides.
- Set a unique `source_name`.

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

## Docker

The repository now includes:

- `Dockerfile`
  - multi-stage build with `server` and `client` targets
- `.env.example`
  - starter environment variables for `docker run --env-file`

### Run the server with `docker run`

1. Build the server image:

```powershell
docker build --target server -t wsjtx-relay-server .
```

2. Create a server env file such as `server.env`:

```dotenv
WSJTX_RELAY_SERVER_LISTEN_ADDR=:8443
WSJTX_RELAY_SERVER_DATA_DIR=/data
WSJTX_RELAY_SERVER_SHARED_SECRET=replace-with-a-long-random-secret
```

3. Start the container:

```powershell
docker run -d `
  --name wsjtx-relay-server `
  -p 8443:8443 `
  -v ${PWD}/docker-data/server:/data `
  --env-file server.env `
  wsjtx-relay-server
```

The server persists its generated certificate and runtime files under the mounted `/data` directory.

### Run the client with `docker run`

1. Build the client image:

```powershell
docker build --target client -t wsjtx-relay-client .
```

2. Create a client env file such as `client.env`:

```dotenv
WSJTX_RELAY_CLIENT_DATA_DIR=/data
WSJTX_RELAY_CLIENT_UDP_LISTEN_ADDR=:2237
WSJTX_RELAY_CLIENT_SERVER_URL=wss://your-server-host:8443
WSJTX_RELAY_CLIENT_SHARED_SECRET=replace-with-a-long-random-secret
WSJTX_RELAY_CLIENT_TENANT_ID=replace-with-a-long-random-tenant-id
WSJTX_RELAY_CLIENT_SOURCE_NAME=station-a
WSJTX_RELAY_CLIENT_SOURCE_DISPLAY_NAME=Station A
WSJTX_RELAY_CLIENT_AUTO_TRUST_ON_FIRST_USE=true
```

3. Start the container:

```powershell
docker run -d `
  --name wsjtx-relay-client `
  -p 2237:2237/udp `
  -v ${PWD}/docker-data/client:/data `
  --env-file client.env `
  wsjtx-relay-client
```

Notes:

- Point WSJT-X or JTDX UDP output to the Docker host on port `2237/udp`.
- The client persists its trusted server fingerprint under the mounted `/data` directory.
- The client uses TOFU, so the first successful connection will trust the server certificate automatically when `WSJTX_RELAY_CLIENT_AUTO_TRUST_ON_FIRST_USE=true`.
- If you want to run both containers on the same Docker network, set `WSJTX_RELAY_CLIENT_SERVER_URL` to the server container name, for example `wss://wsjtx-relay-server:8443`, and add `--network your-network` to both `docker run` commands.

### Build individual images

```powershell
docker build --target server -t wsjtx-relay-server .
docker build --target client -t wsjtx-relay-client .
```

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
