# Authenticated external probes

`tgwp-probe` includes a small MTProxy client. Configure it on a machine outside the
node's hosting network so its report represents that network's real proxy path.
The existing TLS certificate and cover-site HTTP checks remain separate checks.

The bundled client authenticates the FakeTLS handshake or WEB bridge using a
configured test user's proxy secret, sends MTProto `req_pq_multi`, and requires a
well-formed `resPQ` containing the request's random nonce. FakeTLS additionally
verifies the server flight's secret-dependent HMAC. It performs no Telegram login,
creates no account session or authorization key, and sends no Telegram messages.
HTTP 200, a WEB WELCOME, an open port, and a TLS handshake cannot produce an `ok`
protocol result by themselves. This tests the configured DC (2 by default), rather
than asserting availability of every DC, media route, or logged-in user operation.
The unauthenticated MTProto response is a connectivity check, not cryptographic
attestation of Telegram against a deliberately malicious proxy operator.

Create a dedicated test user with access to the transports you want to monitor.
Copy `deploy/probe/protocol.example.json` to `/etc/tgwp-probe/<name>.json`, fill its
secrets privately, and give it mode 0600. Each `faketls` or `web` object is optional;
omitted transports are reported as `not_run`. An empty `{}` enables neither.

- `faketls.secret`: the user's 16-byte secret as exactly 32 hexadecimal characters,
  without the `ee` prefix or appended domain. `sni` is the actual configured
  FakeTLS cover domain; it can differ from `NODE_HOST`.
- `web.secret`: the exact WEB client secret, either 32 hexadecimal characters or
  `dd` followed by 32 hexadecimal characters. It must match the WEB profile's
  `plain` or `dd` secret mode. `ee` is rejected. All four fixed upstream carriers
  (`https`, `https-lanes`, `websocket`, `websocket-lanes`) are supported; the checker
  uses the server-selected fixed carrier, without optional carrier negotiation.
- `web.base_path`: the exact path without leading/trailing slashes, such as
  `telegram/relay`; empty means the root. Its capability is bound to this path.
- `port`: each transport's public port, default 443. WEB uses HTTPS with normal
  certificate validation, and WebSocket carriers use WSS. Redirects are rejected.
- `timeout_seconds`: each transport's budget, default 15 and maximum 30 seconds.
- `dc`: Telegram DC ID from 1 through 5, default 2.

The file must be a private regular file, not a symlink. Unknown fields, malformed
secrets, invalid paths and trailing JSON cause a sanitized configuration error;
the checker never prints credentials or HTTP capabilities. Do not enable request
query, authorization header or WebSocket subprotocol logging on the TLS terminator.

For the provided systemd service with `DynamicUser=yes`, install
`deploy/probe/authenticated.conf.example` as
`/etc/systemd/system/tgwp-probe@<name>.service.d/authenticated.conf`. Its
`LoadCredential` directive lets the dynamic service user read a private copy of the
root-owned secret file. Then run `systemctl daemon-reload`. The existing timer and
`example.env` settings still apply. `TGWP_PROBE_CONFIG` contains only a filename;
proxy credentials never appear in service arguments or environment values.

For a standalone check, use:

```sh
tgwp-probe --host proxy.example.com \
  --protocol-config /private/path/protocol.json --check-only
```

This prints the two sanitized status/latency objects and does not contact the panel.
A completed check may print `failed` while exiting successfully; inspect the JSON
status. Configuration or execution errors exit unsuccessfully. For normal reports,
use the existing `--panel`, `--node`, `--location`, and `TGWP_PROBE_TOKEN` settings.

The existing `--client-check /absolute/executable` adapter remains supported with
its original stdin, JSON, 4096-byte output limit and 45-second timeout. It is
mutually exclusive with `--protocol-config`/`TGWP_PROBE_CONFIG`. Without either
option, both authenticated checks remain `not_run`. Existing custom checkers should
keep their own private credential-file arrangement.

## Verification

Run `go test -race ./cmd/probe ./internal/protocolprobe` for deterministic local
protocol peers, wrong keys, impostors, deadlines, malformed replies and config
validation. The opt-in `TestRealTelemtAuthenticatedExchange` runs inside the isolated
`deploy/run-e2e-telemt.sh` bench after its smoke tests have finished. It reads
`TGWP_PROTOCOL_E2E_CONFIG` (a private file inside the container), checks the real
FakeTLS listener, and supplies a local certificate-verified TLS terminator for the
bench's intentionally private HTTP WEB listener. This is test infrastructure only;
the normal checker always uses the node's public HTTPS/WSS endpoint.

Build and run this opt-in test binary in the retained bench:

```sh
GOOS=linux GOARCH=amd64 go test -c -o /tmp/tgwp-protocol-tests ./internal/protocolprobe
docker cp /tmp/tgwp-protocol-tests <bench-container>:/tmp/tgwp-protocol-tests
docker exec -e TGWP_PROTOCOL_E2E_CONFIG=/private/bench-protocol.json \
  <bench-container> /tmp/tgwp-protocol-tests \
  -test.run TestRealTelemtAuthenticatedExchange -test.v
```

Use the bench's own disposable default user; do not copy production credentials.
The test expects its FakeTLS port/SNI plus a WEB object in the private file, the
bench's WEB listener at `127.0.0.1:18080`, and vhost `fakenode-telemt.local`.
WebSocket carrier runs additionally need that hostname resolving to localhost inside
the test container. Production deployments still need checks from their actual
external probe networks and public TLS terminator.

The implementation follows [telemt 3.5.14 FakeTLS](https://github.com/telemt/telemt/blob/9d5b896bb695c55e2905b82f276da84e45a0a2ec/src/protocol/tls.rs),
[its WEB authentication policy](https://github.com/telemt/telemt/blob/9d5b896bb695c55e2905b82f276da84e45a0a2ec/src/proxy/handshake/auth_candidates.rs),
[its path-bound capability derivation](https://github.com/telemt/telemt/blob/9d5b896bb695c55e2905b82f276da84e45a0a2ec/src/config/load/runtime_web.rs),
the pinned [tproxy WEB v1 contract](https://github.com/telegramdesktop/tproxy-server/blob/52a5feb7fac38f68da5afef9cedd9b3bfc8473ca/PROTOCOL.md),
and Telegram's [MTProto transports](https://core.telegram.org/mtproto/mtproto-transports)
and [initial key exchange](https://core.telegram.org/mtproto/auth_key).
