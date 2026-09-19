# Cronet Browser XHTTP

Set `"browser": true` on an outbound XHTTP transport and build with `with_cronet`.
Supported modes are `packet-up` (default) and `stream-up`. Cronet owns TLS,
HTTP/2 and connection reuse. The outbound dialer supplies TCP connections and
DNS/routing; QUIC is disabled for this integration. `xmux` settings are ignored.
SekaiMod exposes this as `Browser Dialer` under XHTTP for VLESS and EWP profiles.

```json
{
  "type": "xhttp",
  "browser": true,
  "mode": "packet-up",
  "host": "example.com",
  "path": "/xhttp"
}
```

The outbound TLS settings supply `server_name`, optional certificate PEM, ECH,
or REALITY credentials. Cronet performs the handshake and peer authentication.
Browser mode forces certificate verification and removes uTLS, insecure
verification, ALPN overrides other than the native `h2,http/1.1` pair, TLS
fragmentation, spoofing and other unsupported TLS overrides from profile
generated configuration.
Separate download settings, uTLS handshakes, TLS fragmentation/spoofing, client certificates and custom
TLS version/cipher overrides are not supported. Unsupported settings return a
construction error. `no_sse_header` is a server response setting.

ECH supports `tls.ech.config`, `config_path`, or HTTPS-record discovery through
sing-box's DNS router. The actual JSON field for a separate lookup name is
`tls.ech.query_server_name` (not `ech_query_name`):

```json
{
  "enabled": true,
  "server_name": "inner.example.com",
  "ech": {
    "enabled": true,
    "query_server_name": "ech.example.com"
  }
}
```

This is the outbound `tls` object, alongside the `transport` object above. If
`query_server_name` is absent, the lookup uses `server_name`. Configuration is
fetched on dial and cached according to its DNS TTL. Lookup failures, empty
results and removal of an expired record fail the dial. The TCP endpoint remains
the outbound server address, independently of the lookup name, TLS name and
HTTP Host. An in-process DNS socketpair passes the configuration to Chromium's
native ECH handshake; QUIC remains disabled.

Enabling ECH also enables Strict ECH in the client-owned Cronet engine. Chromium
can retry with an authenticated, usable configuration, but empty or unusable
retry lists fail instead of falling back to ordinary TLS. Native tests assert
`ECHAccepted` for successful static, DNS and key-rotation cases in both XHTTP modes,
and capture TLS bytes to check that failure paths do not expose the inner hostname.

## REALITY

Browser supports the existing sing-box XHTTP REALITY server in both modes.
For example, configure a VLESS outbound as follows, replacing its endpoint,
UUID, server name, public key and short ID with the server's settings:

```json
{
  "type": "vless",
  "server": "192.0.2.1",
  "server_port": 443,
  "uuid": "11111111-2222-3333-4444-555555555555",
  "tls": {
    "enabled": true,
    "server_name": "www.example.com",
    "reality": {
      "enabled": true,
      "public_key": "<server X25519 public key in base64url>",
      "short_id": "0102030405060708"
    }
  },
  "transport": {
    "type": "xhttp",
    "browser": true,
    "mode": "packet-up",
    "path": "/xhttp"
  }
}
```

`with_cronet` is sufficient for the client; `tls.utls` and `with_utls` are not
required. For compatibility with existing REALITY profiles, an enabled `utls`
object with an unset or `chrome` fingerprint is accepted, but Cronet still owns
the handshake. Other fingerprint overrides are rejected. The existing sing-box
REALITY **server** still uses `with_utls`.

The native client uses X25519, HKDF-SHA256 and AES-256-GCM to seal the Session ID,
with the same hardcoded `ClientVer=1.8.1` as sing-box. It verifies the certificate's
HMAC before permitting an Ed25519 CertificateVerify that was not advertised by
Chromium. The CertificateVerify signature and TLS Finished are fully verified.
The emitted ClientHello retains Chromium's signature list, hybrid key share,
ALPN/ALPS, extension permutation and ECH GREASE.

The tested sing-box REALITY implementation omits ALPN in its response. Once the
handshake is authenticated, Cronet uses HTTP/2 with prior knowledge if the client
offered h2 and the peer omitted ALPN. An explicit ALPN response takes precedence.
This compatibility behavior requires successful REALITY authentication.

If REALITY authentication fails and the server forwards the connection to its
camouflage website, Cronet verifies the ordinary website certificate and finishes
the TLS handshake. The original XHTTP request then receives
`ERR_REALITY_AUTHENTICATION_FAILED`, while an independent `GET /` visits the
verified SNI on the **same TLS connection**. It uses Cronet's HTTP/2 or HTTP/1.1
stack, the engine's User-Agent and a generated `padding` cookie containing 30–61
zeroes. XHTTP paths, Host overrides, headers, cookies and upload data are never
copied into that request. Without ALPN, the ordinary website uses HTTP/1.1.

The camouflage response is read and discarded before closing the connection,
with a 30-second timeout. Redirects are not followed. Cancelling the failed XHTTP
request does not interrupt this background visit; closing the engine does.
Camouflage connections have private pools and cannot be reused for XHTTP.
Untrusted, expired or wrong-host website certificates and invalid TLS handshake
signatures/Finished messages do not trigger a camouflage request.

REALITY uses an engine dedicated to the configured endpoint. TLS resumption and
0-RTT are disabled, while HTTP/2 connections remain reusable. ECH, custom CA roots,
kTLS and QUIC cannot be combined with Browser REALITY. Unsupported HelloRetryRequest
handshakes fail. REALITY timestamps currently use the native system clock.

## Building and testing

This checkout requires the sibling `../cronet-go` source with Browser XHTTP
and its patched native library (Host authority mapping, Strict ECH and REALITY).
For local development, use the supplied workspace explicitly:

```sh
GOWORK="$PWD/go.work.browser.example" go build -tags 'with_cronet with_purego' ./cmd/sing-box
```

For PureGo, distribute the newly built `libcronet.so` alongside the binary or set
`LD_LIBRARY_PATH`. For CGO, build/package the sibling Cronet library and use its
Chromium compiler environment:

```sh
# Run in ../cronet-go:
go run ./cmd/build-naive --target=linux/amd64 download-toolchain
go run ./cmd/build-naive --target=linux/amd64 build
go run ./cmd/build-naive --target=linux/amd64 package --local
eval "$(go run ./cmd/build-naive --target=linux/amd64 env --export)"

# Then run in sing-box, preserving the exported environment:
GOWORK="$PWD/go.work.browser.example" go build -tags with_cronet ./cmd/sing-box
```

The native integration tests use `with_cronet_test` in addition to the build
tags. They run Browser clients against the real sing-xhttp server over HTTP/2.
Include `with_utls` to run the existing REALITY server inside the tests:

```sh
GOWORK="$PWD/go.work.browser.example" \
LD_LIBRARY_PATH="$PWD/../cronet-go/lib/linux_amd64" \
go test -count=1 -race -timeout 45s \
  -tags 'with_cronet with_purego with_cronet_test with_utls' \
  ./transport/v2rayxhttp ./common/tls ./protocol/vless
```

REALITY tests cover both modes, X25519 and X25519MLKEM768, 2 MB echo, authenticated
HTTP/2 reuse and reconnect, wrong public keys/short IDs, actual Session ID
decryption, and the full VLESS outbound/inbound path without a client uTLS setting.
Wrong-credential cases cover both an untrusted target (no HTTP request) and a
trusted target (independent camouflage GET following the server's TLS forwarding).
Both CGO and PureGo passed on Linux/amd64. Native cryptographic failure tests and
their commands are documented in cronet-go's `test/native/README.md`.

The module version in `go.mod` will need updating once this Cronet implementation
and its native artifacts are published; the workspace makes the local version
explicit during development.

For SekaiMod/Android, the existing `gomobile bind` path should statically link
the Android `libcronet.a` for every selected ABI into `libcore.aar`, with
`with_cronet` enabled and without `with_purego`. No additional `libcronet.so` is
needed. See the sibling [Android integration guide](../../../cronet-go/ANDROID_BROWSER.md).
The Android arm64 native archive, AAR and an API-23 SekaiMod debug APK have been
built and checked separately; see the guide for commands and artifact locations.
Android device/VPN behavior and the other Android ABIs remain unverified.
