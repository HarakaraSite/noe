# ファイル・ソケット・signal

```sh
./noe run examples/low-level/index-roundtrip.js tcp
./noe run examples/low-level/index-roundtrip.js unix
./noe run examples/low-level/wait-signal.js SIGTERM
```

index-roundtripは専用の一時directoryにfileとserverを作り、索引の一部分を読み、socket越しの応答でその部分だけ更新します。送信FIN後も応答を受信します。出力は`[0, 1, 12, 13, 14, 5, 6, 7]`です。Unix socketは実行OSの対応が必要です。

wait-signalは補助timerを使わず待機します。Linux/Darwinでは別端末から表示されたPIDへ`kill -TERM PID`を送ると、一度受信して購読を解除し終了します。SIGINTは既存CLIの取消しも起こすため、この例の正常終了にはSIGTERM等を使います。Windowsの対応名はSIGINTだけです。

## UDP datagram round trip

UDP datagram sockets are available in source after v2.17.0; the released and
installed v2.17.0 binary does not include `bind()`. Build a temporary CLI
from this checkout and run the self-contained loopback example:

```sh
demo_dir="$(mktemp -d)"
trap 'rm -rf "$demo_dir"' EXIT
CGO_ENABLED=0 go build -o "$demo_dir/noe" ./cmd/noe
"$demo_dir/noe" run examples/low-level/udp-roundtrip.js
```

The example binds two sockets to port `0` on `127.0.0.1`, sends and echoes
text, binary, and empty datagrams, and checks both source addresses and packet
contents. It requires no credentials, external service, or filesystem access
from the JavaScript program, and prints exactly `UDP roundtrip passed` to
standard output. A five-second operation timeout keeps an interrupted local
exchange bounded. See [UDP datagram sockets](../../docs/low-level-io.md#udp-datagram-sockets)
for cancellation, packet-size, ownership, and delivery semantics.

## Raw TLS round trip

Raw TLS sockets are available in source after v2.17.0. Build a temporary CLI
from this checkout so the example does not use the installed v2.17.0 binary.
The following commands create a one-day self-signed test identity with OpenSSL.
For an existing certificate, use its paths instead; its subject alternative name
must include `IP:127.0.0.1`. The example reads both PEM files through `noe:fs`.

```sh
demo_dir="$(mktemp -d)"
trap 'rm -rf "$demo_dir"' EXIT
CGO_ENABLED=0 go build -o "$demo_dir/noe" ./cmd/noe
openssl req -x509 -newkey rsa:2048 -nodes \
  -keyout "$demo_dir/key.pem" -out "$demo_dir/cert.pem" \
  -days 1 -subj '/CN=localhost' -addext 'subjectAltName=IP:127.0.0.1'
"$demo_dir/noe" run examples/low-level/tls-roundtrip.js \
  "$demo_dir/cert.pem" "$demo_dir/key.pem"
```

The TLS example binds a port-0 listener, completes a verified client
handshake, sends text and binary bytes, and receives an echo after sending TLS
`close_notify`. It prints `TLS roundtrip passed` and exits. See the
[raw TLS contract](../../docs/low-level-io.md#raw-tls-sockets) for the Go host
option and stream shutdown details.

[公開API・所有と停止](../../docs/low-level-io.md) · [例の一覧](../README.md)
