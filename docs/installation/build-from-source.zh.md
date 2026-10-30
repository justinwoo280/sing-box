---
icon: material/file-code
---

# 从源代码构建

## :material-graph: 要求

### 此分支

Go 1.25.5 或更高版本。发版 CI 使用 Go 1.26.10。

### sing-box 1.11

* Go 1.23.1 - ~

### sing-box 1.10

* Go 1.20.0 - ~
* Go 1.21.0 - ~ with tag `with_ech` enabled

### sing-box 1.9

* Go 1.18.5 - 1.22.x
* Go 1.20.0 - 1.22.x with tag `with_quic`, or `with_utls` enabled
* Go 1.21.0 - 1.22.x with tag `with_ech` enabled

您可以从 https://go.dev/doc/install 下载并安装 Go，推荐使用最新版本。

## :material-fast-forward: 快速开始

```bash
make
```

或者构建二进制文件并将其安装到 `$GOBIN`：

```bash
make install
```

## :material-cog: 自定义构建

```bash
TAGS="tag_a tag_b" make
```

or

```bash
go build -tags "tag_a tag_b" ./cmd/sing-box
```

## :material-folder-settings: 构建标记

| 构建标记                               | 默认启动              | 说明                                                                                                                                                                                                                                                                                                                             |
|------------------------------------|-------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `with_quic`                        | :material-check:  | Build with QUIC support, see [QUIC and HTTP3 DNS transports](/zh/configuration/dns/server/), [Naive inbound](/zh/configuration/inbound/naive/), [Hysteria Inbound](/zh/configuration/inbound/hysteria/), [Hysteria Outbound](/zh/configuration/outbound/hysteria/) and [V2Ray Transport#QUIC](/zh/configuration/shared/v2ray-transport#quic). |
| `with_grpc`                        | :material-close:️ | Build with standard gRPC support, see [V2Ray Transport#gRPC](/zh/configuration/shared/v2ray-transport#grpc).                                                                                                                                                                                                                      |
| `with_dhcp`                        | :material-check:  | Build with DHCP support, see [DHCP DNS transport](/zh/configuration/dns/server/).                                                                                                                                                                                                                                                 |
| `with_wireguard`                   | :material-check:  | Build with WireGuard support, see [WireGuard outbound](/zh/configuration/outbound/wireguard/).                                                                                                                                                                                                                                    |
| `with_utls`                        | :material-check:  | Build with [uTLS](https://github.com/refraction-networking/utls) support for TLS outbound, see [TLS](/zh/configuration/shared/tls#utls).                                                                                                                                                                                          |
| `with_acme`                        | :material-check:  | Build with ACME TLS certificate issuer support, see [TLS](/zh/configuration/shared/tls/).                                                                                                                                                                                                                                         |
| `with_clash_api`                   | :material-check:  | Build with Clash API support, see [Experimental](/zh/configuration/experimental#clash-api-fields).                                                                                                                                                                                                                                |
| `with_v2ray_api`                   | :material-close:️ | Build with V2Ray API support, see [Experimental](/zh/configuration/experimental#v2ray-api-fields).                                                                                                                                                                                                                                |
| `with_gvisor`                      | :material-check:  | Build with gVisor support, see [Tun inbound](/zh/configuration/inbound/tun#stack) and [WireGuard outbound](/zh/configuration/outbound/wireguard#system_interface).                                                                                                                                                                   |
| `with_embedded_tor` (CGO required) | :material-close:️ | Build with embedded Tor support, see [Tor outbound](/zh/configuration/outbound/tor/).                                                                                                                                                                                                                                             |
| `with_tailscale`                   | :material-check:  | 构建 Tailscale 支持，参阅 [Tailscale 端点](/zh/configuration/endpoint/tailscale)。                                                                                                                                                                                                                                                         |
| `with_ccm`                         | :material-check:  | 构建 Claude Code Multiplexer 服务支持。                                                                                                                                                                                                                                                                                              |
| `with_ocm`                         | :material-check:  | 构建 OpenAI Codex Multiplexer 服务支持。                                                                                                                                                                                                                                                                                             |
| `with_naive_outbound`              | :material-check:  | 构建 NaiveProxy 出站支持，参阅 [NaiveProxy 出站](/zh/configuration/outbound/naive/)。                                                                                                                                                                                                                                                         |
| `with_cloudflared`                 | :material-check:  | 构建 Cloudflare Tunnel 入站支持，参阅 [Cloudflared 入站](/zh/configuration/inbound/cloudflared/)。                                                                                                                                                                                                                                              |
| `badlinkname`                      | :material-check:  | 启用 `go:linkname` 以访问标准库内部函数。Go 标准库未提供本项目需要的许多底层 API，且在外部重新实现不切实际。用于 kTLS（内核 TLS 卸载）和原始 TLS 记录操作。                                                                                                                                                                                                                           |
| `tfogo_checklinkname0`             | :material-check:  | `badlinkname` 的伴随标记。Go 1.23+ 链接器强制限制 `go:linkname` 使用；此标记表示构建使用 `-checklinkname=0` 以绕过该限制。                                                                                                                                                                                                                                |

除非您确实知道您正在启用什么，否则不建议更改默认构建标签列表。

## lwIP

默认 TUN 栈为 `go`。添加 `with_lwip` 可提供 `"stack": "lwip"`，并选择一种原生绑定模式：

| 目标 | CGO | 附加标签 | 原生库 |
|---|---|---|---|
| Windows／macOS／Linux glibc 桌面 | `0` | `with_purego` | 可执行文件旁的 DLL／dylib／so |
| Linux musl／IoT | `1` | `with_lwip_prebuilt,with_musl` | 静态链接到可执行文件 |
| Android API 23+ | `1` | `with_lwip_prebuilt` | 静态链接到 CLI 或 libbox |
| 旧 CPU ABI／Android API 21 | `1` | 准备源码后使用 CGO；musl 加 `with_musl` | 按具体目标编译锁定源码 |

`with_purego` 同时影响 Cronet，静态构建应省略它。预编译静态库仍需要目标 C 工具链。
Linux 完全静态程序使用 musl 和外部 `-static` 链接；Android 仍依赖系统 libc。

`release/build-native.sh` 将匹配的动态库、构建元数据和许可证一起放入 `dist`。
支持 Cronet 的目标可设置 `WITH_CRONET=true`。例如，在仓库根目录执行：

```sh
GOOS=linux GOARCH=amd64 LWIP_MODE=dynamic WITH_CRONET=true bash release/build-native.sh
GOOS=linux GOARCH=amd64 CC="bash $PWD/release/zig-cc.sh -target x86_64-linux-musl" \
  LWIP_MODE=static LWIP_LIBC=musl WITH_CRONET=true bash release/build-native.sh
```

`LWIP_MODE=source` 会在临时目录下载并校验模块锁定的原生源码，使用临时 modfile 构建，
不改动仓库中的依赖文件。普通 `go build` 不会自动下载 C 源码。

现有 Linux ARM32 预编译库要求 ARMv7、NEON 和 hard-float，MIPS 预编译库要求 hard-float。
发版工作流为 ARMv5/v6、无 NEON 的通用 ARMv7、386 softfloat 和 MIPS softfloat 的 IoT 目标使用源码 CGO。
ARMv7 NEON 的 musl 变体同时包含 Cronet。
没有匹配动态库的旧桌面变体仍提供 `go`、`system`、`gvisor` 和 `mixed`，不包含 lwIP。

## :material-wrench: 链接器标志

官方构建所需的链接器标志维护在 `release/LDFLAGS` 中。下游构建应原样使用该文件。

| 标志                                                          | 说明                                                                                                                                                         |
|-------------------------------------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `-checklinkname=0`                                          | Go 1.23+ 链接器拒绝未授权的 `go:linkname` 使用。此标志禁用该检查，需要与 `badlinkname` 构建标记一起使用。                                                                                   |

## :material-package-variant: 下游打包者

默认构建标签列表和链接器标志以文件形式存放在仓库中，供下游打包者直接引用：

| 文件 | 说明 |
|------|------|
| `release/DEFAULT_BUILD_TAGS` | Linux（常见架构）、Darwin 和 Android 的默认标签。 |
| `release/DEFAULT_BUILD_TAGS_WINDOWS` | Windows 的默认标签（包含 `with_purego`）。 |
| `release/DEFAULT_BUILD_TAGS_OTHERS` | 其他平台的默认标签（不含 `with_naive_outbound`）。 |
| `release/LDFLAGS` | 必需的链接器标志（参见上文）。 |

## :material-layers: with_naive_outbound

NaiveProxy 出站需要根据目标平台进行特殊的构建配置。

### 支持的平台

| 平台           | 架构                                                       | 模式     | 要求                                                  |
|--------------|----------------------------------------------------------|--------|-----------------------------------------------------|
| Linux        | amd64, arm64                                             | purego | 无（官方发布版本已包含库文件）                                     |
| Linux        | 386, amd64, arm, arm64, mipsle, mips64le, riscv64, loong64 | CGO    | Chromium 工具链，运行时需要 glibc >= 2.31（loong64: >= 2.36） |
| Linux (musl) | 386, amd64, arm, arm64, mipsle, riscv64, loong64          | CGO    | Chromium 工具链                                        |
| Windows      | amd64, arm64                                             | purego | 无（官方发布版本已包含库文件）                                     |
| Apple 平台     | *                                                        | CGO    | Xcode                                               |
| Android      | *                                                        | CGO    | Android NDK                                          |

### Windows

使用 `with_purego` 标记。

官方发布版本已包含 `libcronet.dll`。自行构建时，从 [cronet-go releases](https://github.com/sagernet/cronet-go/releases) 下载并放置在 `sing-box.exe` 相同目录或 `PATH` 中的任意目录。

### Linux (purego, 仅 amd64/arm64)

使用 `with_purego` 标记。

官方发布版本已包含 `libcronet.so`。自行构建时，从 [cronet-go releases](https://github.com/sagernet/cronet-go/releases) 下载并放置在 sing-box 二进制文件相同目录或系统库路径中。

### Linux (CGO)

参阅 [cronet-go](https://github.com/sagernet/cronet-go#linux-build-instructions)。

- **glibc 构建**：运行时需要 glibc >= 2.31
- **musl 构建**：使用 `with_musl` 标记，静态链接，无运行时要求

### Apple 平台 / Android

参阅 [cronet-go](https://github.com/sagernet/cronet-go)。
