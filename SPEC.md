# Gamelight

Multiplayer game streaming server that connects to Sunshine and fans out video/audio to multiple web clients via WebRTC.

## Architecture

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                              Gamelight Server                                  │
│                                                                                │
│  ┌─────────────────┐    ┌─────────────────┐    ┌─────────────────────────┐   │
│  │  Sunshine       │    │  RTSP/RTP       │    │  WebRTC Fan-out         │   │
│  │  Client         │───▶│  Receiver       │───▶│  (Pion)                 │   │
│  │                 │    │                 │    │                         │   │
│  │  - Pairing      │    │  - H.264/H.265  │    │  - Video Track (1→N)    │   │
│  │  - Launch/Resume│    │  - AV1          │    │  - Audio Track (1→N)    │   │
│  │  - Server Info  │    │  - Opus Audio   │    │  - Data Channels        │   │
│  └─────────────────┘    └─────────────────┘    └───────────┬─────────────┘   │
│          ▲                                                  │                 │
│          │                                                  │                 │
│  ┌───────┴─────────┐    ┌─────────────────┐                │                 │
│  │  Input Handler  │◀───│  Session        │◀───────────────┘                 │
│  │                 │    │  Manager        │                                   │
│  │  - Gamepads 0-3 │    │                 │                                   │
│  │  - KB/Mouse P1  │    │  - Players 1-4  │                                   │
│  └─────────────────┘    │  - Spectators   │                                   │
│                         │  - Permissions  │                                   │
│                         └─────────────────┘                                   │
└──────────────────────────────────────────────────────────────────────────────┘
                                    │
                            WebRTC + WebSocket
                                    │
            ┌───────────────────────┼───────────────────────┐
            │                       │                       │
     ┌──────┴──────┐         ┌──────┴──────┐         ┌──────┴──────┐
     │  Player 1   │         │  Player 2   │         │  Spectator  │
     │  (Host)     │         │             │         │             │
     │  KB+Mouse   │         │  Gamepad    │         │  View Only  │
     │  +Gamepad   │         │             │         │             │
     └─────────────┘         └─────────────┘         └─────────────┘
```

## Components

### 1. Sunshine Client (`pkg/sunshine/`)
Pure Go implementation of the Moonlight/Sunshine protocol:
- **Server Info**: Query Sunshine for host info, app list, codec support
- **Pairing**: 5-step challenge-response pairing protocol
- **Launch/Resume**: Start streaming sessions with specified settings
- **Cancel**: Stop streaming sessions

### 2. RTSP/RTP Receiver (`pkg/rtsp/`)
Receives video/audio streams from Sunshine:
- RTSP session management
- RTP packet parsing for video (H.264, H.265, AV1)
- RTP packet parsing for audio (Opus)
- Frame assembly from RTP packets

### 3. WebRTC Fan-out (`pkg/webrtc/`)
Pion-based WebRTC for multi-client streaming:
- Single video/audio source → multiple peer connections
- Video track broadcasting to all connected clients
- Audio track broadcasting to all connected clients
- Data channels for bidirectional communication (input, control)
- ICE/STUN/TURN for NAT traversal

### 4. Session Manager (`pkg/session/`)
Manages streaming sessions and players:
- Single active streaming session
- Player slots 1-4 with assigned gamepads
- Unlimited spectators (view-only)
- Host (Player 1) controls:
  - Toggle keyboard/mouse for other players
  - Kick players
  - End session

### 5. Input Handler (`pkg/input/`)
Collects and routes input from clients:
- Gamepad input from browser Gamepad API
- Keyboard events (host or permitted players only)
- Mouse events (host or permitted players only)
- Routes input to correct Sunshine gamepad slot

### 6. Web Server (`pkg/web/`)
HTTP server and WebSocket signaling:
- Static file serving for web UI
- WebSocket endpoint for WebRTC signaling
- REST API for session state
- No authentication (minimal setup)

## Protocol Details

### Sunshine/Moonlight Protocol (HTTP/HTTPS + XML)

**Base URLs:**
- HTTP: `http://sunshine:47989`
- HTTPS: `https://sunshine:47984`

**Endpoints:**
- `GET /serverinfo` - Server information and capabilities
- `GET /applist` - List of available applications
- `GET /pair` - Pairing flow (5 phases)
- `GET /unpair` - Remove pairing
- `GET /launch` - Start streaming an application
- `GET /resume` - Resume existing stream
- `GET /cancel` - Stop streaming

### Streaming Protocol (RTSP/RTP)

After launch, Sunshine provides an RTSP session URL:
1. Connect to RTSP endpoint
2. Receive SDP describing video/audio streams
3. Video sent as RTP packets (port 47998)
4. Audio sent as RTP packets (port 48000)
5. Control messages via ENet (port 47999)

### Input Protocol

Input sent to Sunshine via encrypted UDP:
- AES-128-GCM encrypted packets
- Key negotiated during launch (rikey)
- Packet types: mouse, keyboard, controller, touch

## Web UI

Single page application with minimal UI:

### Index Page
- Auto-connects to default Desktop stream
- Fullscreen video player
- Collapsible side panel for:
  - Stream quality settings (bitrate, resolution, FPS)
  - Player list with join/spectate status
  - Host controls (if Player 1):
    - Toggle KB/Mouse for players
    - Kick players
  - "Join as Player" button (for spectators)

### Session Flow
1. First visitor starts stream → becomes Player 1 (Host)
2. Subsequent visitors join as spectators
3. Spectators click "Join as Player" to get a player slot (2-4)
4. Players can spectate again by clicking "Spectate"
5. When host leaves, session ends (or transfers to Player 2)

## Configuration

```yaml
# config.yaml
sunshine:
  host: "localhost"
  http_port: 47989
  https_port: 47984
  # Pre-configured pairing (optional)
  client_cert: "./certs/client.pem"
  client_key: "./certs/client.key"

webrtc:
  ice_servers:
    - urls: ["stun:stun.l.google.com:19302"]
    - urls: ["turn:your-turn-server.com:3478"]
      username: "user"
      credential: "pass"
  port_range:
    min: 40000
    max: 40100

server:
  bind_address: "0.0.0.0:8080"
  # Enable HTTPS with certificates
  tls_cert: ""
  tls_key: ""

stream:
  default_app: "Desktop"
  default_bitrate: 10000  # kbps
  default_fps: 60
  default_width: 1920
  default_height: 1080
```

## Dependencies

- **pion/webrtc**: WebRTC implementation for Go
- **pion/rtp**: RTP packet handling
- **pion/sdp**: SDP parsing
- **gorilla/websocket**: WebSocket support
- **go-chi/chi**: HTTP router

## Build & Run

```bash
# Build
go build -o gamelight ./cmd/gamelight

# Run
./gamelight --config config.yaml

# Or with defaults
./gamelight --sunshine-host localhost
```

## API Endpoints

### WebSocket: `/ws`
WebRTC signaling and session management.

**Client → Server:**
```json
{"type": "offer", "sdp": "..."}
{"type": "ice_candidate", "candidate": {...}}
{"type": "join_as_player"}
{"type": "spectate"}
{"type": "set_quality", "bitrate": 10000, "fps": 60}
```

**Server → Client:**
```json
{"type": "answer", "sdp": "..."}
{"type": "ice_candidate", "candidate": {...}}
{"type": "session_state", "players": [...], "you": {...}}
{"type": "stream_started"}
{"type": "error", "message": "..."}
```

### REST: `/api/session`
Get current session state.

```json
GET /api/session
{
  "active": true,
  "app": "Desktop",
  "players": [
    {"slot": 1, "name": "Host", "is_host": true},
    {"slot": 2, "name": "Player2", "is_host": false}
  ],
  "spectators": 5,
  "quality": {
    "bitrate": 10000,
    "fps": 60,
    "width": 1920,
    "height": 1080
  }
}
```

## Future Enhancements

- [ ] Multiple concurrent sessions (different apps)
- [ ] Audio chat between players
- [ ] Screen annotations/drawing
- [ ] Recording/replay
- [ ] Mobile touch controls
- [ ] Password-protected sessions
