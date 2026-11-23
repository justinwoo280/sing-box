V2Ray Transport is a set of private protocols invented by v2ray, and has contaminated the names of other protocols, such
as `trojan-grpc` in clash.

### Structure

```json
{
  "type": ""
}
```

Available transports:

* HTTP
* WebSocket
* QUIC
* gRPC
* HTTPUpgrade
* [XHTTP](#xhttp)

!!! warning "Difference from v2ray-core"

    * No TCP transport, plain HTTP is merged into the HTTP transport.
    * No mKCP transport.
    * No DomainSocket transport.

!!! note ""

    You can ignore the JSON Array [] tag when the content is only one item

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

!!! warning "Difference from v2ray-core"

    TLS is not enforced. If TLS is not configured, plain HTTP 1.1 is used.

#### host

List of host domain.

The client will choose randomly and the server will verify if not empty.

#### path

!!! warning

    V2Ray's documentation says that the path between the server and the client must be consistent, 
    but the actual code allows the client to add any suffix to the path.
    sing-box uses the same behavior as V2Ray, but note that the behavior does not exist in `WebSocket` and `HTTPUpgrade` transport.

Path of HTTP request.

The server will verify.

#### method

Method of HTTP request.

The server will verify if not empty.

#### headers

Extra headers of HTTP request.

The server will write in response if not empty.

#### idle_timeout

In HTTP2 server:

Specifies the time until idle clients should be closed with a GOAWAY frame. PING frames are not considered as activity.

In HTTP2 client:

Specifies the period of time after which a health check will be performed using a ping frame if no frames have been
received on the connection.Please note that a ping response is considered a received frame, so if there is no other
traffic on the connection, the health check will be executed every interval. If the value is zero, no health check will
be performed.

Zero is used by default.

#### ping_timeout

In HTTP2 client:

Specifies the timeout duration after sending a PING frame, within which a response must be received.
If a response to the PING frame is not received within the specified timeout duration, the connection will be closed.
The default timeout duration is 15 seconds.

### XHTTP

This section describes builds of this fork's `1.14.x` branch with the `sing-xhttp` integration. XHTTP is the `transport` of a VLESS, VMess, Trojan, or EWP inbound or outbound, not an inbound or outbound type of its own.

#### Minimal configuration

Add this snippet to both the inbound and the outbound. Keep their protocol type and authentication settings. Use the same base path on both sides.

```json
{
  "transport": {
    "type": "xhttp",
    "mode": "packet-up",
    "path": "/tunnel"
  }
}
```

For EWP, keep `type: "ewp"` on the inbound and outbound. Match `server_id` and the client `uuid` with the server's `server_id` and `users[].uuid`. `sing-box generate ewp-keypair` generates the server's `signing_private_key` and the client's `server_public_key`. These are EWP fields, not XHTTP headers. The outer `tls` object is a sibling of `transport`.

#### Modes and HTTP versions

| `mode` | Upload and download | Client HTTP versions |
| --- | --- | --- |
| `packet-up` | Separate upload requests carrying numbered packets, plus a download GET. | HTTP/1.1, HTTP/2, HTTP/3 |
| `stream-down` | The same wire format as `packet-up`; the GET can use its own `download` settings. | HTTP/1.1, HTTP/2, HTTP/3 |
| `stream-up` | One streaming upload request and a separate download GET. | HTTP/2 or HTTP/3 for the upload; the GET can use HTTP/1.1 on a separate transport. |
| `stream-one` | One bidirectional request. No separate GET or `download`. | HTTP/2 or HTTP/3 |
| `auto` | Ordinary clients select `packet-up`. A REALITY client selects `stream-one`, or `stream-up` when `download` is present. A server accepts all wire modes compatible with its framing settings. | Depends on the selected mode. |

Omitting `mode` uses `auto` selection on the client and `packet-up` on the server. Set the server to `auto` explicitly if it needs to accept several modes.

Ordinary XHTTP uses HTTP/1.1 without TLS. With [TLS](./tls.md), the client uses HTTP/2 by default. Put `http/1.1` first in `tls.alpn` for HTTP/1.1 over TLS, or `h3` first for HTTP/3. For a sing-box HTTP/3 inbound, use `tls.alpn: ["h3"]` and allow UDP on the listen port. HTTP/3 requires standard TLS; it does not support uTLS or REALITY. Ordinary REALITY uses HTTP/2.

#### Custom headers and padding example

Merge this example into an existing outbound if the server or its reverse proxy expects these settings. The domains, auth value, and tuning values are examples, not defaults.

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

#### HTTP fields

| Field | Meaning |
| --- | --- |
| `host` | HTTP Host (or `:authority`), not the dial address or TLS `server_name`. On a client, the default is the TLS server name if available, otherwise the server address. An inbound with a nonempty `host` checks it. |
| `path` | Base path, default `/`. Match the base path on the inbound. A client can add a query, e.g. `/tunnel?route=up`; the server's base path remains `/tunnel`. |
| `headers` | Custom request headers on clients and response headers on servers. Each value can be a string or an array of strings. An inbound `Authorization` entry does **not** authenticate client requests. |
| `uplink_http_method` | Client upload method, default `POST`. This does not change the separate GET. `OPTIONS` and `CONNECT` are not allowed, and `stream-up` cannot use `GET` for upload. |
| `no_grpc_header` | Client option to suppress the automatic `Content-Type: application/grpc` on streaming uploads. Default `false`. |
| `no_sse_header` | Server option to suppress the automatic `Content-Type: text/event-stream` on the download response. Default `false`. |
| `grpc_framing` | Optional sing-xhttp framing extension, default `false`. Use matching settings on both peers. |

An `application/grpc` header or gRPC-style User-Agent does **not** in itself enable gRPC framing. With `grpc_framing: true`, use `stream-up` or `stream-one`, `POST`, a request body, and keep the gRPC headers enabled. Use a single service path, e.g. `/tunnel`, without a query string or query padding on the RPC path. The upload goes to `/tunnel/Tun`. In `stream-up`, it carries the session ID in `X-Xhttp-Session`; the GET still uses normal XHTTP session placement.

#### Wire options

Range fields accept an integer (`100`), a string (`"100"` or `"100-300"`), or an object (`{"from":100,"to":300}`). Bounds are inclusive and must satisfy `0 <= from <= to <= 2147483647`. A fixed zero (`0` or `"0-0"`) selects the field's default or leaves its limit unset.

| Field | Meaning |
| --- | --- |
| `x_padding_bytes` | Padding range; default `100-1000`. |
| `x_padding_obfs_mode` | Default `false`: the client writes repeated `X` into the `x_padding` query parameter of a `Referer` URL. The next four fields apply only when enabled. |
| `x_padding_placement` | `header` (default when enabled), `query`, `cookie`, or `query-in-header` (`queryInHeader` is also accepted). |
| `x_padding_key` | Key for `query`, `cookie`, or `query-in-header`; default `x_padding`. Not used to carry the value with `header` placement. |
| `x_padding_header` | Header name for `header` or `query-in-header`; default `X-Padding`. With `header` placement, this header carries the generated padding directly. Do not put a fixed `Auth-Token` in `headers` to configure padding. |
| `x_padding_method` | `repeat-x` (default) or `tokenish`. For `tokenish`, the range targets the HPACK Huffman-encoded size, not the character count. |
| `session_placement` / `session_key` | Put the session ID in `path` (default), `query`, `header` or `cookie`. The default key is `X-Session` for headers and `x_session` for queries/cookies. |
| `seq_placement` / `seq_key` | Packet upload sequence placement: `path` (default), `query`, `header`, or `cookie`; the default key is `X-Seq` for headers and `x_seq` for queries/cookies. Do not use the same non-path key as for the session ID in the same location. |
| `session_id_table` / `session_id_length` | Client session ID generation. A literal character table (at most 256 bytes) or a predefined name such as `number`, `hex`, or `Base62` plus a positive length or range replaces the default UUID. `stream-one` has no session ID. |
| `uplink_data_placement` | Packet payload: `body` (default), `header`, `cookie`, or `auto` (the client sends a body; a server in `auto` placement accepts header, cookie, and body data). Streaming uploads use the body. |
| `uplink_data_key` | Required for `header`, `cookie`, or `auto` data placement. Base64 chunks use `<key>-0`, `<key>-1`, … for headers or `<key>_0`, `<key>_1`, … for cookies. |
| `uplink_chunk_size` | Range for the number of encoded characters per header or cookie chunk. Defaults: `3000-4000` for headers and `2048-3072` for cookies. |

For a direct connection, match the wire settings (padding, metadata placement, and payload format) to the server. Use the field names on this page in sing-box JSON: for example `x_padding_bytes`, `uplink_http_method`, `xmux.max_concurrency`, and `download`, not `xPaddingBytes`, `uplinkHTTPMethod`, `xmux.maxConcurrency`, or `downloadSettings`.

#### Tuning

| Field | Meaning |
| --- | --- |
| `sc_max_each_post_bytes` | Upload packet size range; the server uses its upper bound as the packet limit. Default `1000000` bytes. |
| `sc_min_posts_interval_ms` | Client spacing between packet uploads, default `30` milliseconds. Range. |
| `sc_max_buffered_posts` | Server reorder queue limit, integer, default `30`. |
| `sc_stream_up_server_secs` | Server heartbeat interval range on the `stream-up` upload response, default `20-80` seconds. |
| `server_max_header_bytes` | Inbound request header limit, integer in bytes. Default `8192` for TCP-based HTTP; HTTP/3 uses the HTTP library's default. |

`xmux` tunes the ordinary XHTTP client connection pool. Omit it to keep the existing defaults.

| Field in `xmux` | Meaning |
| --- | --- |
| `max_concurrency` | Per-connection session concurrency threshold (range, e.g. `"2-9"`). |
| `max_connections` | Target size of the connection pool (range). Not a hard cap when all connections are busy. |
| `c_max_reuse_times` | Connection pick budget (range). |
| `h_max_request_times` | Request budget (range). |
| `h_max_reusable_secs` | Reuse lifetime in seconds (range). |
| `h_keep_alive_period` | HTTP/2 PING interval, integer in seconds. `0` or omission uses `30`; `-1` disables PINGs. |

#### Independent download

Add `download` inside the outbound's `transport` object for an independent GET in `packet-up`, `stream-up`, or `stream-down`. Omit it to keep the shared uplink/downlink configuration. This example changes the destination and path, clears the inherited custom headers and XMUX settings, and sends the session ID in a downlink query parameter.

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

| Field in `download` | If omitted or explicitly set |
| --- | --- |
| `server`, `server_port` | Omit both to use the upload destination, or set both to select a separate destination. |
| `tls` | Uses the [outbound TLS object](./tls.md). If a separate `server` is specified and `tls` is omitted, the download uses **cleartext HTTP**. With no separate server, omitting `tls` inherits upload TLS. `{"enabled":false}` explicitly disables TLS. |
| `detour`, `domain_resolver` | `detour` is the tag of a different outbound; without it the upload dialer policy applies. [`domain_resolver`](./dial.md#domain_resolver) can override resolution for the download server. With a different detour and no resolver override, that outbound handles the destination. |
| `host`, `path` | Omit `path` to keep the upload path and query. A new path replaces both. Omit `host` to use the upload Host on the same endpoint or derive it from the download TLS/address for a separate server. |
| `headers` | Omission inherits the upload custom headers, **including `Authorization`** if set. An object replaces them; `{}` clears them. Transport-generated headers can still be sent. |
| `session_placement`, `session_key` | Omitted fields inherit. Set `session_key` to `""` to use the default key for the selected placement. These fields change how the same session ID is sent, not how it is generated. |
| `x_padding_*` | Omitted fields inherit. `x_padding_obfs_mode: false` restores normal `Referer` padding; it does not disable padding. `x_padding_bytes: 0` restores the default range instead of inheriting it. |
| `xmux` | Omission inherits the upload tuning. An object replaces it; `{}` uses default tuning on a separate download pool. A separate server, dialer or TLS configuration also gets its own pool. Host/path or header overrides alone do not require one. |

The upload and GET must reach the **same XHTTP session** on the server. Two independent sing-box inbounds do not share sessions. If different frontends use different paths or session placements, configure them to forward both requests with matching metadata to the same XHTTP backend.

#### Browser

Omit `browser` or set it to `false` for the ordinary XHTTP configuration above. `browser: true` selects the existing Cronet-based client in builds that support it. It does not support `download` or the ordinary-only uplink method, obfuscated padding, session/sequence, or uplink payload placement settings. Browser manages its own pool; `xmux` does not tune it.

Use `sing-box check -c config.json` to validate the assembled configuration. This checks the configuration, not the remote route or authentication.

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

Path of HTTP request.

The server will verify.

#### headers

Extra headers of HTTP request.

The server will write in response if not empty.

#### max_early_data

Allowed payload size is in the request. Enabled if not zero.

#### early_data_header_name

Early data is sent in path instead of header by default.

To be compatible with Xray-core, set this to `Sec-WebSocket-Protocol`.

It needs to be consistent with the server.

### QUIC

```json
{
  "type": "quic"
}
```

!!! warning "Difference from v2ray-core"

    No additional encryption support:
    It's basically duplicate encryption. And Xray-core is not compatible with v2ray-core in here.

### gRPC

!!! note ""

    standard gRPC has good compatibility but poor performance and is not included by default, see [Installation](/installation/build-from-source/#build-tags).

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

Service name of gRPC.

#### idle_timeout

In standard gRPC server/client:

If the transport doesn't see any activity after a duration of this time,
it pings the client to check if the connection is still active.

In default gRPC server/client:

It has the same behavior as the corresponding setting in HTTP transport.

#### ping_timeout

In standard gRPC server/client:

The timeout that after performing a keepalive check, the client will wait for activity.
If no activity is detected, the connection will be closed.

In default gRPC server/client:

It has the same behavior as the corresponding setting in HTTP transport.

#### permit_without_stream

In standard gRPC client:

If enabled, the client transport sends keepalive pings even with no active connections.
If disabled, when there are no active connections, `idle_timeout` and `ping_timeout` will be ignored and no keepalive
pings will be sent.

Disabled by default.

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

Host domain.

The server will verify if not empty.

#### path

Path of HTTP request.

The server will verify.

#### headers

Extra headers of HTTP request.

The server will write in response if not empty.
