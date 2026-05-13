# wsjtx-relay 中文说明

`wsjtx-relay` 是一套用于转发 `WSJT-X` / `JTDX` 实时数据的轻量级 relay 方案。

适合以下场景：

- 手机不在电台所在的局域网内
- 希望一个台站被多个远端观察端同时查看
- 希望 `WsjtxWatcher` 通过远端 relay 获取数据，而不是直接收 UDP

仓库同时包含服务端和客户端：

- `wsjtx-relay-server`
  - 公网或私网 relay 服务
  - 接收 `ingest` 和 `watch` 两类 WebSocket 会话
- `wsjtx-relay-client`
  - 本地桥接客户端
  - 监听 WSJT-X UDP，并把事件推送到 relay 服务端

## 工作方式

1. `wsjtx-relay-client` 在本地监听 WSJT-X / JTDX 的 UDP 数据。
2. 客户端把 WSJT-X 消息转换成 protobuf relay 帧。
3. 客户端通过 `wss://` 把这些帧发送到 `wsjtx-relay-server`。
4. `WsjtxWatcher` 作为 `watch` 客户端连接服务端。
5. watcher 选择某个 source 后，持续接收实时 decode、status、session、QSO 等事件。

这个 relay 面向实时状态转发，不提供历史回放。

## 目录结构

- `cmd/wsjtx-relay-client`
  - 客户端入口
- `cmd/wsjtx-relay-server`
  - 服务端入口
- `internal/client`
  - 客户端配置、relay 连接、TOFU 信任存储
- `internal/server`
  - 服务端配置、运行时、广播逻辑、TLS 工具
- `internal/shared`
  - 共享认证逻辑和 WebSocket envelope 工具
- `configs`
  - 示例 YAML 配置

## 安全模型

- 传输层使用基于 TLS 的 WebSocket。
- 服务端可自动生成并持久化自签名证书。
- 客户端使用 TOFU 模式，在首次成功连接时保存服务端 SPKI 指纹。
- 认证使用 `shared_secret` 加带时间窗限制的 HMAC proof。
- 租户隔离依赖 `tenant_id` 和 `shared_secret` 的组合。
- 当前实现并不是“服务端为每个 tenant 单独保存一把密钥”的模型。
- 单帧 relay 消息大小当前限制为 `1 MiB`。

需要特别注意：

- `tenant_id` 必须视为高熵、私密的配置项，不是给人看的房间名。
- `public: true` 会完全关闭认证，只适合测试或可信网络环境。

## 服务端配置

示例配置在 `configs/server.example.yaml`：

```yaml
listen_addr: "0.0.0.0:8443"
data_dir: ./data
shared_secret: "replace-with-a-strong-secret"
heartbeat_interval: 10s
heartbeat_timeout: 30s
max_timestamp_skew: 90s
```

主要配置项：

- `listen_addr`
  - HTTPS / WebSocket 监听地址
- `data_dir`
  - 运行数据目录，用于保存生成的 TLS 文件等
- `shared_secret`
  - relay 认证共享密钥；除非 `public: true`，否则必填
- `public`
  - 设为 `true` 后关闭认证，变成开放 relay
- `heartbeat_interval`
  - 服务端发给客户端的应用层心跳间隔
- `heartbeat_timeout`
  - 多久没有收到有效帧就断开连接
- `max_timestamp_skew`
  - 认证时间戳允许的最大偏差

二进制还支持以下可选配置：

- `cert_file`
  - 已存在的 TLS 证书路径
- `key_file`
  - 已存在的 TLS 私钥路径

行为说明：

- 如果未配置证书，服务端会自动生成一个自签名证书，并输出 SPKI 指纹。
- 当 `public: true` 时，不要求 `shared_secret`，同时也不会做认证。
- `public: true` 的含义是：任何能连到服务端的人都可以作为 `ingest` 或 `watch` 连接进来。

## 客户端配置

示例配置在 `configs/client.example.yaml`：

```yaml
udp_listen_addr: ":2237"
server_url: "wss://example.com:8443"
shared_secret: "replace-me"
tenant_id: "replace-with-a-random-shared-id"
source_name: "station-a"
```

### 最小必需配置

基本跑起来只需要：

```yaml
udp_listen_addr: ":2237"
server_url: "wss://example.com:8443"
shared_secret: "replace-me"
tenant_id: "replace-with-a-random-shared-id"
source_name: "station-a"
```

可省略项：

- `data_dir`
  - 不填时默认使用 `./data`
- `trust_store_path`
  - 不填时默认保存在 `data_dir/trusted_server_fingerprint.txt`
- `auto_trust_on_first_use`
  - 不填时默认是 `true`
- `client_name`
  - 不填时默认是 `wsjtx-relay-client`
- `client_version`
  - 不填时默认使用构建版本号
- `instance_id`
  - 不填时启动时随机生成

### 客户端配置说明

- `udp_listen_addr`
  - 用于接收 WSJT-X / JTDX 数据的本地 UDP 地址
- `server_url`
  - relay 服务端基础地址，应使用 `wss://host:port`
- `shared_secret`
  - 必须和服务端配置一致
- `tenant_id`
  - relay 客户端和 watcher 之间共享的高熵私密 tenant 标识
  - 它既用于 tenant 路由，也参与认证计算
  - 必须使用足够长的随机值，不要使用 `home`、`test`、`station1` 这类人类可猜的名字
- `source_name`
  - 同一 tenant 内的逻辑 source 名称
- `source_display_name`
  - 展示给 watcher 的友好名称
- `trust_store_path`
  - 保存已信任服务端指纹的文件路径
- `auto_trust_on_first_use`
  - 设为 `true` 时，首次看到的服务端指纹会自动信任并保存
- `instance_id`
  - 可选的稳定实例标识
  - 如果希望客户端重启时平滑替换上一条 ingest 会话，这个值会有用

## WsjtxWatcher 配置

在 `WsjtxWatcher` 中，进入设置，把数据源切换为 `Third-party data source`，然后配置：

- `Server URL`
  - relay 服务 watch 端点的基础地址，例如 `wss://example.com:8443`
- `Shared Secret`
  - 与 relay 客户端、服务端一致
- `Tenant ID`
  - 与 relay 客户端相同的高熵私密 tenant ID
  - 它既决定你进入哪个 tenant 命名空间，也参与认证
- `Select source`
  - 选择要观看的 relay source
- `Refresh source list`
  - 重连并刷新 source 列表
- `Re-pair server`
  - 清空本地 TOFU 信任，让应用重新配对服务端证书

首次成功连接后，`WsjtxWatcher` 会自动保存观察到的服务端指纹。

## Docker 部署

仓库已经包含服务端用的 `Dockerfile` 和 `docker-compose.yml` 示例。

### 直接运行镜像

先准备一个服务端配置文件，例如 `./server.yaml`：

```yaml
listen_addr: "0.0.0.0:8443"
data_dir: /data
shared_secret: "replace-with-a-strong-secret"
heartbeat_interval: 10s
heartbeat_timeout: 30s
max_timestamp_skew: 90s
```

然后启动容器：

```bash
docker run -d \
  --name wsjtx-relay-server \
  --restart unless-stopped \
  -p 8443:8443 \
  -v "$(pwd)/server.yaml:/etc/wsjtx-relay/server.yaml:ro" \
  -v "$(pwd)/data:/data" \
  sydneymrcat/wsjtx-relay-server \
  --config /etc/wsjtx-relay/server.yaml \
  --data-dir /data
```

说明：

- 建议把 `/data` 挂载到宿主机持久化目录，避免容器重启后丢失自动生成的 TLS 文件
- 配置中的 `listen_addr` 应设置为 `0.0.0.0:8443`，这样容器内服务才能正常对外监听
- 如果使用自签名证书，客户端首次成功连接后会按现有 TOFU 逻辑保存服务端指纹

### 使用 Docker Compose

仓库内置的 `docker-compose.yml` 就是按这个思路组织的：挂载一个配置文件，再挂载一个持久化数据目录。

示例：

```yaml
services:
  wsjtx-relay-server:
    image: sydneymrcat/wsjtx-relay-server
    container_name: wsjtx-relay-server
    restart: unless-stopped
    ports:
      - "8443:8443"
    volumes:
      - ./server.yaml:/etc/wsjtx-relay/server.yaml:ro
      - ./data:/data
    command: ["--config", "/etc/wsjtx-relay/server.yaml", "--data-dir", "/data"]
```

启动命令：

```bash
docker compose up -d
```

### 本地构建镜像

如果你想自己构建服务端镜像，而不是直接使用已发布镜像：

```bash
docker build -t wsjtx-relay-server:local .
```

## 快速开始

### 1. 启动服务端

```powershell
go run ./cmd/wsjtx-relay-server --config ./configs/server.example.yaml
```

请务必把 `shared_secret` 改成强随机值。  
如果你打算跳过认证，可以使用 `--public`，但不要在公网开放环境里这样做，除非你明确就是要一个无认证开放 relay。

### 2. 在电台机器上配置并启动客户端

- 把 WSJT-X 或 JTDX 的 UDP 输出指向 `udp_listen_addr`
- 把 `server_url` 指向 relay 服务端
- 使用相同的 `shared_secret`
- 选择一个足够长、随机的 `tenant_id`
  - 可以直接生成随机 hex、base32 或 base64url 字符串
  - 把它当成私密配置，不要把它当展示名称使用
- 设置唯一的 `source_name`

然后启动客户端：

```powershell
go run ./cmd/wsjtx-relay-client --config ./configs/client.example.yaml
```

### 3. 配置 `WsjtxWatcher`

- 打开 `Settings`
- 把 `Data source` 切换为 `Third-party data source`
- 填写：
  - `Server URL`
  - `Shared Secret`
  - `Tenant ID`
- 保存设置
- 从主界面启动 watcher 服务
- 打开 `Select source` 并选择目标 source

## 运维建议

- 不要把 `shared_secret` 和 `tenant_id` 发到群聊、工单、截图或公开日志里。
- 如果任一值泄露，建议把该 tenant 的 `shared_secret` 和 `tenant_id` 一起轮换。
- 如果服务要暴露到远端网络，优先放在防火墙、Tailscale、WireGuard 或带连接控制的反向代理后面。
- 当前设计更适合自托管和可信环境，不建议直接当成“多租户公网 SaaS”来理解。

## 常用命令

```powershell
go test ./...
go run ./cmd/wsjtx-relay-server --config ./configs/server.example.yaml
go run ./cmd/wsjtx-relay-client --config ./configs/client.example.yaml
```

## 备注

- Go module 路径：`github.com/sydneyowl/wsjtx-relay`
- protobuf 依赖来自同级 `wsjtx-relay-proto` 仓库
- 该 relay 面向实时事件转发，不做历史补发
