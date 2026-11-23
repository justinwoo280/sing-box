V2Ray Transport 是 v2ray 发明的一组私有协议，并污染了其他协议的名称，如 clash 中的 `trojan-grpc`。

### 结构

```json
{
  "type": ""
}
```

可用的传输协议：

* HTTP
* WebSocket
* QUIC
* gRPC
* HTTPUpgrade
* [XHTTP](#xhttp)

!!! warning "与 v2ray-core 的区别"

    * 没有 TCP 传输层, 纯 HTTP 已合并到 HTTP 传输层。
    * 没有 mKCP 传输层。
    * 没有 DomainSocket 传输层。

!!! note ""

    当内容只有一项时，可以忽略 JSON 数组 [] 标签。

### HTTP

```json
{
  "type": "http",
  "host": [],
  "path": "",
  "method": "",
  "headers": {},
  "idle_timeout": "15s",
  "ping_timeout": "15s"
}
```

!!! warning "与 v2ray-core 的区别"

    不强制执行 TLS。如果未配置 TLS，将使用纯 HTTP 1.1。

#### host

主机域名列表。

如果设置，客户端将随机选择，服务器将验证。

#### path

!!! warning

    V2Ray 文档称服务端和客户端的路径必须一致，但实际代码允许客户端向路径添加任何后缀。
    sing-box 使用与 V2Ray 相同的行为，但请注意，该行为在 `WebSocket` 和 `HTTPUpgrade` 传输层中不存在。

HTTP 请求路径

服务器将验证。

#### method

HTTP 请求方法

如果设置，服务器将验证。

#### headers

HTTP 请求的额外标头

如果设置，服务器将写入响应。

#### idle_timeout

在 HTTP2 服务器中：

指定闲置客户端应在多长时间内使用 GOAWAY 帧关闭。PING 帧不被视为活动。

在 HTTP2 客户端中：

如果连接上没有收到任何帧，指定一段时间后将使用 PING 帧执行健康检查。需要注意的是，PING 响应被视为已接收的帧，因此如果连接上没有其他流量，则健康检查将在每个间隔执行一次。如果值为零，则不会执行健康检查。

默认使用零。

#### ping_timeout

在 HTTP2 客户端中：

指定发送 PING 帧后，在指定的超时时间内必须接收到响应。如果在指定的超时时间内没有收到 PING 帧的响应，则连接将关闭。默认超时持续时间为 15 秒。

### WebSocket

```json
{
  "type": "ws",
  "path": "",
  "headers": {},
  "max_early_data": 0,
  "early_data_header_name": ""
}
```

#### path

HTTP 请求路径

服务器将验证。

#### headers

HTTP 请求的额外标头

如果设置，服务器将写入响应。

#### max_early_data

请求中允许的最大有效负载大小。默认启用。

#### early_data_header_name

默认情况下，早期数据在路径而不是标头中发送。

要与 Xray-core 兼容，请将其设置为 `Sec-WebSocket-Protocol`。

它需要与服务器保持一致。

### QUIC

```json
{
  "type": "quic"
}
```

!!! warning "与 v2ray-core 的区别"

    没有额外的加密支持：
    它基本上是重复加密。 并且 Xray-core 在这里与 v2ray-core 不兼容。

### gRPC

!!! note ""

    默认安装不包含标准 gRPC (兼容性好，但性能较差), 参阅 [安装](/zh/installation/build-from-source/#构建标记)。

```json
{
  "type": "grpc",
  "service_name": "TunService",
  "idle_timeout": "15s",
  "ping_timeout": "15s",
  "permit_without_stream": false
}
```

#### service_name

gRPC 服务名称。

#### idle_timeout

在标准 gRPC 服务器/客户端：

如果传输在此时间段后没有看到任何活动，它会向客户端发送 ping 请求以检查连接是否仍然活动。

在默认 gRPC 服务器/客户端：

它的行为与 HTTP 传输层中的相应设置相同。

#### ping_timeout

在标准 gRPC 服务器/客户端：

经过一段时间之后，客户端将执行 keepalive 检查并等待活动。如果没有检测到任何活动，则会关闭连接。

在默认 gRPC 服务器/客户端：

它的行为与 HTTP 传输层中的相应设置相同。

#### permit_without_stream

在标准 gRPC 客户端：

如果启用，客户端传输即使没有活动连接也会发送 keepalive ping。如果禁用，则在没有活动连接时，将忽略 `idle_timeout` 和 `ping_timeout`，并且不会发送 keepalive ping。

默认禁用。

### HTTPUpgrade

```json
{
  "type": "httpupgrade",
  "host": "",
  "path": "",
  "headers": {}
}
```

#### host

主机域名。

服务器将验证。

#### path

HTTP 请求路径

服务器将验证。

#### headers

HTTP 请求的额外标头。

如果设置，服务器将写入响应。

### XHTTP

本节适用于本分支 `1.14.x` 中接入 `sing-xhttp` 的构建。XHTTP 是 VLESS、VMess、Trojan 或 EWP 入站/出站的 `transport`，不是独立的入站或出站类型。

#### 最小配置

在入站和出站中分别加入以下片段，保留原有的协议类型和认证字段，两边使用相同的基础路径。

```json
{
  "transport": {
    "type": "xhttp",
    "mode": "packet-up",
    "path": "/tunnel"
  }
}
```

以 EWP 为例，入站和出站外层仍为 `type: "ewp"`。客户端的 `server_id` 和 `uuid` 分别与服务端的 `server_id` 和 `users[].uuid` 匹配。用 `sing-box generate ewp-keypair` 生成服务端的 `signing_private_key` 和客户端的 `server_public_key`。这些是 EWP 字段，不是 XHTTP 请求头。外层 `tls` 与 `transport` 同级。

#### 模式与 HTTP 版本

| `mode` | 上行与下行 | 客户端支持的 HTTP 版本 |
| --- | --- | --- |
| `packet-up` | 通过多个请求上传带序号的分包，使用独立 GET 下载。 | HTTP/1.1、HTTP/2、HTTP/3 |
| `stream-down` | 与 `packet-up` 的传输格式相同；可用 `download` 单独配置 GET。 | HTTP/1.1、HTTP/2、HTTP/3 |
| `stream-up` | 使用一个流式上传请求和一个独立下载 GET。 | 上行需要 HTTP/2 或 HTTP/3；独立下行可以使用 HTTP/1.1。 |
| `stream-one` | 使用单个双向请求。没有独立 GET，不支持 `download`。 | HTTP/2 或 HTTP/3 |
| `auto` | 普通客户端选择 `packet-up`；REALITY 客户端选择 `stream-one`，配置了 `download` 则选择 `stream-up`。服务端接受与其 framing 设置相容的所有模式。 | 取决于实际选中的模式。 |

省略 `mode` 时，客户端按 `auto` 选择，服务端使用 `packet-up`。服务端需要接收多种模式时，显式设为 `auto`。

普通 XHTTP 未启用 TLS 时使用明文 HTTP/1.1。启用 [TLS](./tls.zh.md) 时客户端默认使用 HTTP/2；若要使用 TLS + HTTP/1.1，将 `http/1.1` 放在 `tls.alpn` 首项；HTTP/3 则将 `h3` 放在首项。sing-box 的 HTTP/3 入站需要设置 `tls.alpn: ["h3"]`，并放行监听端口的 UDP。HTTP/3 需要标准 TLS，不支持 uTLS 或 REALITY。普通 REALITY 使用 HTTP/2。

#### 自定义请求头与填充示例

如果服务端或前置反向代理要求这些设置，将以下字段合并到现有出站。域名、认证值和调优值仅为示例，不是默认项。

```json
{
  "server": "upload.example.org",
  "server_port": 443,
  "tls": { "enabled": true, "server_name": "upload.example.org", "alpn": ["h2", "http/1.1"] },
  "transport": {
    "type": "xhttp",
    "mode": "stream-up",
    "host": "upload.example.org",
    "path": "/tunnel?route=up",
    "headers": {
      "User-Agent": "example-client/1.0",
      "Authorization": "Bearer example-only",
      "X-Example": ["one", "two"]
    },
    "x_padding_bytes": "100-300",
    "x_padding_obfs_mode": true,
    "x_padding_placement": "header",
    "x_padding_header": "Auth-Token",
    "x_padding_method": "tokenish",
    "xmux": { "max_concurrency": "2-9" }
  }
}
```

#### HTTP 字段

| 字段 | 说明 |
| --- | --- |
| `host` | HTTP Host（或 `:authority`），不是实际拨号地址，也不改变 TLS `server_name`。客户端未设置时优先使用 TLS 服务器名，否则使用服务器地址；入站配置了 `host` 时会校验它。 |
| `path` | 基础路径，默认为 `/`。客户端可附加查询字符串，例如 `/tunnel?route=up`；入站基础路径仍填写 `/tunnel`。 |
| `headers` | 客户端发送的自定义请求头，或服务端写入的响应头。值可以是字符串或字符串数组。在入站中配置 `Authorization` **不等于**校验来访请求。 |
| `uplink_http_method` | 客户端上传方法，默认 `POST`。不改变独立下行的 `GET`。不支持 `OPTIONS`、`CONNECT`；`stream-up` 也不能用 `GET` 上传。 |
| `no_grpc_header` | 客户端选项，关闭流式上传自动添加的 `Content-Type: application/grpc`。默认 `false`。 |
| `no_sse_header` | 服务端选项，关闭下载响应自动添加的 `Content-Type: text/event-stream`。默认 `false`。 |
| `grpc_framing` | sing-xhttp 的可选 gRPC 封装扩展，默认 `false`，需要两端使用相同设置。 |

仅设置 `application/grpc` 请求头或 gRPC 风格的 User-Agent **不会**启用 gRPC 封装。显式设置 `grpc_framing: true` 时，使用 `stream-up` 或 `stream-one`，上传方法为 `POST`，通过请求体传输并保留 gRPC 请求头。将 `path` 配成单个服务名，例如 `/tunnel`；RPC 路径不支持查询字符串或查询参数填充。上传将访问 `/tunnel/Tun`；`stream-up` 的上传在 `X-Xhttp-Session` 中携带会话 ID，下行 GET 仍使用普通 XHTTP 的会话位置。

#### 传输格式

范围字段支持整数（`100`）、字符串（`"100"` 或 `"100-300"`）和对象（`{"from":100,"to":300}`）。范围包含端点，满足 `0 <= from <= to <= 2147483647`。固定为零（`0` 或 `"0-0"`）表示使用该字段的默认值或不设限制。

| 字段 | 说明 |
| --- | --- |
| `x_padding_bytes` | 填充范围，默认 `100-1000`。 |
| `x_padding_obfs_mode` | 默认 `false`，客户端在 `Referer` URL 的 `x_padding` 查询参数中填入重复的 `X`。下方四项仅在启用后生效。 |
| `x_padding_placement` | `header`（启用后的默认值）、`query`、`cookie` 或 `query-in-header`（也接受 `queryInHeader`）。 |
| `x_padding_key` | `query`、`cookie` 或 `query-in-header` 的键名，默认 `x_padding`；`header` 模式不使用此键来传送填充值。 |
| `x_padding_header` | `header` 或 `query-in-header` 的请求头名，默认 `X-Padding`。`header` 模式将动态生成的填充直接写入该请求头，不要在 `headers` 中填一个固定的 `Auth-Token` 来配置填充。 |
| `x_padding_method` | `repeat-x`（默认）或 `tokenish`。`tokenish` 的范围针对 HPACK Huffman 编码后的字节数，不是字符数。 |
| `session_placement` / `session_key` | 会话 ID 的位置：`path`（默认）、`query`、`header` 或 `cookie`。请求头默认键名为 `X-Session`，查询参数和 Cookie 默认键名为 `x_session`。 |
| `seq_placement` / `seq_key` | 上传分包序号的位置：`path`（默认）、`query`、`header` 或 `cookie`。请求头默认键名为 `X-Seq`，查询参数和 Cookie 默认键名为 `x_seq`。在同一个非路径位置，序号不要与会话 ID 使用同名键。 |
| `session_id_table` / `session_id_length` | 客户端生成会话 ID 的字符表和长度。可以用直接填写的字符表（最多 256 字节）或 `number`、`hex`、`Base62` 等预设，再指定正整数长度或长度范围，代替默认的 UUID。`stream-one` 不使用会话 ID。 |
| `uplink_data_placement` | 分包载荷位置：`body`（默认）、`header`、`cookie` 或 `auto`（客户端使用请求体；服务端在 `auto` 位置模式下接受三种位置的数据）。流式上传使用请求体。 |
| `uplink_data_key` | `header`、`cookie` 或 `auto` 位置必须设置。Base64 分块的请求头形如 `<key>-0`、`<key>-1`，Cookie 形如 `<key>_0`、`<key>_1`。 |
| `uplink_chunk_size` | 请求头或 Cookie 的单块编码字符数范围。请求头默认 `3000-4000`，Cookie 默认 `2048-3072`。 |

直连时让填充、会话/序号位置和载荷格式与服务端一致。在 sing-box JSON 中使用本页字段名，例如 `x_padding_bytes`、`uplink_http_method`、`xmux.max_concurrency` 和 `download`，而非 `xPaddingBytes`、`uplinkHTTPMethod`、`xmux.maxConcurrency` 或 `downloadSettings`。

#### 传输调优

| 字段 | 说明 |
| --- | --- |
| `sc_max_each_post_bytes` | 上传分包大小范围；服务端取其上界作为允许的分包上限。默认 `1000000` 字节。 |
| `sc_min_posts_interval_ms` | 客户端分包上传的间隔范围，默认 `30` 毫秒。 |
| `sc_max_buffered_posts` | 服务端乱序队列上限，整数，默认 `30`。 |
| `sc_stream_up_server_secs` | 服务端在 `stream-up` 上传响应上发送心跳的间隔范围，默认 `20-80` 秒。 |
| `server_max_header_bytes` | 入站请求头大小上限，单位为字节，整数。TCP 上的 HTTP 默认为 `8192`，HTTP/3 使用 HTTP 库的默认值。 |

`xmux` 用于调节普通 XHTTP 客户端的 HTTP 连接池。省略即可保持原有默认行为。

| `xmux` 内的字段 | 说明 |
| --- | --- |
| `max_concurrency` | 单连接会话并发阈值（范围，例如 `"2-9"`）。 |
| `max_connections` | 连接池目标大小（范围）。所有连接繁忙时不是硬上限。 |
| `c_max_reuse_times` | 连接可被选用的次数（范围）。 |
| `h_max_request_times` | HTTP 请求数限制（范围）。 |
| `h_max_reusable_secs` | 连接可复用时间（秒，范围）。 |
| `h_keep_alive_period` | HTTP/2 PING 间隔（秒，整数）。省略或设为 `0` 时使用 `30`，设为 `-1` 可关闭。 |

#### 独立下行

在出站的 `transport` 内加入 `download`，即可为 `packet-up`、`stream-up` 或 `stream-down` 的 GET 单独配置下行。省略时保持上下行共用配置。以下示例将下行指向另一个地址和路径，不继承自定义请求头和 XMUX 调优，并在查询参数中传递会话 ID。

```json
{
  "download": {
    "server": "download.example.net",
    "server_port": 443,
    "tls": { "enabled": true, "server_name": "download.example.net", "alpn": ["h2"] },
    "host": "download.example.net",
    "path": "/down?route=2",
    "headers": {},
    "session_placement": "query",
    "session_key": "download_sid",
    "x_padding_obfs_mode": false,
    "x_padding_bytes": 0,
    "xmux": {}
  }
}
```

| `download` 内的字段 | 省略和显式设置的区别 |
| --- | --- |
| `server`、`server_port` | 同时省略则使用上传的拨号地址；需要独立地址时两项一起设置。 |
| `tls` | 使用标准[出站 TLS 对象](./tls.zh.md)。指定独立 `server` 却省略 `tls` 时，下行是**明文 HTTP**。未设置独立地址，且省略 `tls` 时，继承上传 TLS。`{"enabled":false}` 显式禁用 TLS。 |
| `detour`、`domain_resolver` | `detour` 指定不同出站的标签；省略时使用上传的拨号策略。可用 [`domain_resolver`](./dial.zh.md#domain_resolver) 单独指定下行域名解析；指定了不同的 `detour` 且未覆写解析器时，由所选出站处理目的地。 |
| `host`、`path` | 省略 `path` 继承上传路径和查询字符串；指定新路径则整体替换。省略 `host` 时，同端点使用上传的 Host；指定独立地址则从下行 TLS 名称或地址派生。 |
| `headers` | 省略时继承上行的自定义请求头，**包括已配置的 `Authorization`**。显式对象整体替换；`{}` 表示清空自定义头，不影响协议自动生成的请求头。 |
| `session_placement`、`session_key` | 省略时逐项继承；将 `session_key` 设为 `""` 可采用所选位置的默认键名。只改变同一个会话 ID 的传送方式，不重新生成 ID。 |
| `x_padding_*` | 省略时逐项继承。`x_padding_obfs_mode: false` 恢复普通 `Referer` 填充，不是关闭填充；`x_padding_bytes: 0` 将范围恢复为默认值，而不是继承上行范围。 |
| `xmux` | 省略时继承上传调优；显式对象整体替换，`{}` 表示使用默认值并为下行创建独立池。独立地址、拨号器或 TLS 也会创建独立池；仅覆写 Host、路径或请求头不需要独立池。 |

上传请求和下行 GET 必须在服务端汇合到**同一个 XHTTP 会话**。两个独立 sing-box 入站不会共享会话。如果前置代理为两个方向提供不同路径或会话位置，需要将其转换为同一个后端所接受的格式，再转发到同一 XHTTP 后端。

#### Browser

省略 `browser` 或设置为 `false`，使用上述普通 XHTTP。`browser: true` 在支持的构建中选用现有 Cronet 客户端，不支持 `download`、普通 XHTTP 的自定义上传方法、填充混淆、会话/序号及上传载荷位置。Browser 管理自己的连接池，`xmux` 不调整该连接池。

合并配置后可用 `sing-box check -c config.json` 做配置校验；该命令不验证远端路由或认证结果。
