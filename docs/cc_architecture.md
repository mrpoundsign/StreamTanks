# StreamTanks — Command & Control (C&C) & Remote Networking Architecture

## 1. Goal & Component Topology

StreamTanks provides interactive remote controls and game state synchronization for Twitch stream overlays, mobile devices, and native desktop clients. Because streamers run their game host behind residential NATs and firewalls without port forwarding, an intermediary **Command & Control (C&C) Relay Server** coordinates real-time state and viewer actions.

```
+-------------------------------------------------------------------------+
|                              VIEWERS                                    |
|   Twitch Extension (Desktop Overlay & Mobile) / Future Native Clients   |
|                 (Protobuf binary frames over WSS)                       |
+------------------------------------+------------------------------------+
                                     |
                                     | wss://st-cc.poundsigndesign.com/ws/viewer
                                     v
+-------------------------------------------------------------------------+
|                     CLOUDFLARE SSL TERMINATION (TCP 443)                |
+------------------------------------+------------------------------------+
                                     |
                                     v
+-------------------------------------------------------------------------+
|                         C&C RELAY SERVER                                |
|                        (cmd/ccserver)                                   |
|   - Authenticates Twitch Extension JWTs (ViewerAuth)                    |
|   - Authenticates Broadcasters via in-chat %claim & HMAC tokens         |
|   - Dynamic Translation: Legacy JSON <--> Binary Protobuf               |
|   - WebSocket Hub & Channel Multiplexer                                 |
+------------------------------------+------------------------------------+
                                     |
                                     | wss://st-cc.poundsigndesign.com/ws/host
                                     | (format=proto for v0.3.0+ / JSON for legacy)
                                     v
+-------------------------------------------------------------------------+
|                  LOCAL STREAMTANKS GAME INSTANCE                        |
|                  (cmd/streamtanks on streamer PC)                       |
|   - Authoritative Game Loop, Physics & Terrain Deformation              |
|   - Outbound C&C Client (internal/app/cc_client.go)                     |
|   - OBS Browser Source Overlay (public/game.js, 1920x1080)              |
+-------------------------------------------------------------------------+
```

---

## 2. Networking Decisions: Cloudflare & WebSockets over TCP

### Why Not QUIC / UDP / WebTransport?
During architecture planning, QUIC and WebTransport over UDP were evaluated for ultra-low latency. However, production constraints made QUIC unfeasible:
1. **Cloudflare SSL Termination ("Orange Cloud")**: Standard Cloudflare reverse-proxying terminates TCP HTTPS/WSS on port 443. Cloudflare automatically drops raw UDP traffic on custom ports (such as UDP 4433) unless paying for Enterprise Cloudflare Spectrum.
2. **WebTransport Edge Support**: WebTransport proxying from Cloudflare edge to arbitrary origin servers is not available on standard plans.
3. **Certificate Maintenance**: Bypassing Cloudflare would require managing custom TLS certificates and dynamic DNS directly on origin servers, introducing significant operational friction.

### The Solution: Protocol Buffers over Secure WebSockets (`wss://`)
By running binary Protocol Buffer frames over standard WebSockets (`wss://`):
- All traffic proxies cleanly through Cloudflare on standard TCP port 443 with automated SSL termination and DDoS mitigation.
- Binary framing eliminates JSON serialization overhead, base64 encoding bloat, and redundant field key repetition.
- Latency remains negligible (<20ms relay overhead), easily accommodating 60fps input timer updates and smooth action dispatching.

---

## 3. Protocol Buffers Schema & Tooling

The schema is defined in [`proto/streamtanks/v1/game.proto`](../proto/streamtanks/v1/game.proto).

### Core Schema Types
- **`ViewerState`**: High-level synchronized match state, phase, timer, winner, player lists, tank entities, and terrain heightmap.
- **`TankState`**: Real-time snapshot of active tanks (`id`, `username`, `x`, `y`, `angle`, `health`, `is_bot`, `color`, `is_shielded`, `shield_used`).
- **`ViewerContext`**: Identity synchronization sent immediately to newly connected viewers (`username`, `channel_id`, `opaque_user_id`, `twitch_user_id`).
- **`ViewerServerMessage`**: Top-level union envelope (`oneof payload { ViewerState state = 1; ViewerContext context = 2; }`) sent from C&C server to viewers.
- **`ViewerAuthMessage`**: Handshake envelope sent by viewers carrying the Twitch Extension JWT (`jwt`).
- **`ViewerActionMessage`**: Typed action envelope sent by viewers (`oneof action { FireAction, MoveAction, ShieldAction, JoinAction, LeaveAction, StartMatchAction }`).

### Terrain Resolution: Full 1920-Point Heightmap
During the `INPUT` aiming phase, the server broadcasts the full 1920-point `int32` terrain heightmap array (`terrain`).
- **No Downscaling**: Downscaling (e.g. to 240 or 480 samples) was explicitly avoided. A full 1920-integer array serialized with Protocol Buffers varint encoding consumes only ~1.5 to 2.5 KB per frame.
- **Client Zoom & Inspection**: This provides mobile and desktop clients the fidelity required to implement pinch-to-zoom and inspect precise crater lip geometry, tank angles, and firing arcs without interpolation artifacts.

### Build Tooling: Buf
Protobuf compilation is managed via [Buf](https://buf.build/):
- **Configuration**: [`buf.yaml`](../buf.yaml) and [`buf.gen.yaml`](../buf.gen.yaml).
- **Go Generation**: Uses `protoc-gen-go` to generate Go structs in `internal/proto/streamtanks/v1/game.pb.go`.
- **TypeScript Generation**: Uses `@bufbuild/protoc-gen-es` to generate ES module classes in `ext-web/src/proto/streamtanks/v1/game_pb.ts`.
- **One-Command Generation**:
  ```bash
  npm run proto:gen
  ```

---

## 4. Dual-Compatibility: Supporting Legacy Game Servers

### The Constraint
Because the Twitch Extension is hosted centrally on Twitch CDN, all viewers immediately run the latest extension bundle. However, streamers run desktop instances of `StreamTanks.exe` and may update on different schedules. Therefore, **`ccserver` must transparently interoperate between legacy JSON servers and modern Protobuf extension clients.**

### Translation Mechanics in `cmd/ccserver`
`ccserver` acts as a bidirectional translation bridge:

1. **Legacy Host -> Modern Viewer**:
   - Legacy servers connect without `format=proto` and send standard JSON `GAME_STATE` text frames:
     `{"type": "GAME_STATE", "payload": {"phase": "INPUT", "timer_remaining": 15, "players": [...]}}`
   - In `hub.go`, [`jsonStateToProtoBytes`](../cmd/ccserver/hub.go) detects the JSON payload, unpacks its fields, converts them into a `streamtankspbv1.ViewerState`, and marshals a binary `ViewerServerMessage` Protobuf frame.
   - Modern Twitch extension clients receive binary Protobuf frames seamlessly.
   - Terrain and tanks arrays are omitted in legacy payloads; [ext.ts](../ext-web/src/ext.ts) handles their absence gracefully.

2. **Modern Viewer -> Legacy Host**:
   - Viewers tap Fire, Move, Shield, Join, Leave, or Start Match in the modern extension, sending binary `ViewerActionMessage` frames.
   - In `viewer.go`, `ccserver` unpacks the Protobuf action and converts it into the canonical JSON `EXTENSION_COMMAND` envelope:
     ```json
     {
       "type": "EXTENSION_COMMAND",
       "payload": {
         "user": "alice",
         "twitch_id": "123456",
         "command": {
           "type": "CHAT_COMMAND",
           "payload": "%fire 45 60"
         }
       }
     }
     ```
   - Legacy servers receive this exact JSON format and invoke `processCommand()` as if the command were received from Twitch IRC chat.

3. **Modern Host -> Legacy Viewer**:
   - If an un-updated or third-party client connects without `format=proto`, `ccserver` unmarshals the host's Protobuf binary frame into JSON and dispatches a standard `GAME_STATE` JSON text frame.

---

## 5. WebSocket Frame Type Discrimination (`frameCodec`)

### The Gotcha with `golang.org/x/net/websocket`
In `golang.org/x/net/websocket`, a `*websocket.Conn` has a `PayloadType` field that is set once during connection creation (defaults to `TextFrame = 0x01`). **It is never updated when reading frames.** Standard calls to `websocket.Message.Receive(ws, &data)` do not reveal whether the incoming frame was binary or text.

### The Solution: `wsFrame` and `frameCodec`
To accurately discriminate incoming frames, [`cmd/ccserver/hub.go`](../cmd/ccserver/hub.go) implements a custom Codec:

```go
type wsFrame struct {
    payloadType byte
    data        []byte
}

var frameCodec = websocket.Codec{
    Marshal: func(v any) ([]byte, byte, error) { ... },
    Unmarshal: func(data []byte, payloadType byte, v any) error {
        if f, ok := v.(*wsFrame); ok {
            f.payloadType = payloadType
            f.data = data
            return nil
        }
        return errors.New("unsupported target type")
    },
}
```

By receiving into `wsFrame`, `ccserver` inspects `frame.payloadType` directly from the WebSocket frame header (`0x01` Text vs `0x02` Binary):
- If `BinaryFrame`: unmarshal as Protobuf.
- If `TextFrame`: unmarshal as JSON.

---

## 6. Host Authentication & Pairing Handshake

To prevent malicious users from hijacking a streamer's channel on `ccserver`, host registration uses a cryptographic claim workflow:

1. **Initial Connection**: Host dials `wss://st-cc.poundsigndesign.com/ws/host?channel=<channel>&format=proto`.
2. **Challenge Generation**: If no valid token is supplied, `ccserver` issues an `AUTH_CHALLENGE` JSON message containing a randomized 6-character claim code (e.g. `XRB2WS`).
3. **In-Chat Verification**: The local game logs a prompt instructing the streamer to type `%claim <CODE>` in their own Twitch chat. The `ccserver`'s embedded Twitch IRC bot listens in that channel.
4. **Token Issuance**: When the verified channel broadcaster posts the claim code in chat, `ccserver` marks the host as authenticated and issues a cryptographically signed HMAC token:
   `<channel>.<timestamp>.<hmac_hex>`
5. **Persistence**: The host saves this token in SQLite (`cc_host_token`). Subsequent connections authenticate immediately via `?token=<token>`.

---

## 7. Future Native Clients (Android / Desktop)

The adoption of Protocol Buffers over WebSockets establishes the foundation for standalone client applications (e.g. native Android controller or standalone desktop viewer):
- Clients connect directly to `wss://st-cc.poundsigndesign.com/ws/viewer?format=proto`.
- They compile the exact same [`proto/streamtanks/v1/game.proto`](../proto/streamtanks/v1/game.proto) schema using Kotlin, Swift, or C#.
- Full map inspection, protractor angle/power aiming, and action dispatching work identically to the Twitch Extension without code duplication.
