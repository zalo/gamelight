# Gamelight

Multiplayer game streaming server that connects to [Sunshine](https://github.com/LizardByte/Sunshine) and streams video/audio to multiple web clients via WebRTC.

## Features

- **Multi-player Support**: Up to 4 players can connect simultaneously
- **Spectator Mode**: Unlimited spectators can watch without taking player slots
- **WebRTC Streaming**: Low-latency video/audio using Pion WebRTC
- **Gamepad Support**: Browser Gamepad API mapped to controller slots
- **Host Controls**: Player 1 can manage permissions for other players
- **Simple Setup**: Single binary, minimal configuration

## Architecture

```
Sunshine ◄──[Moonlight Protocol]──► Gamelight ◄──[WebRTC]──► Browser Clients
         (RTSP/RTP video/audio)                (Video/Audio tracks)
         (Control messages)                    (Data channels for input)
```

Gamelight acts as a bridge between Sunshine and web browsers:
1. Connects to Sunshine using the Moonlight protocol
2. Receives video/audio streams via RTSP/RTP
3. Fans out streams to multiple WebRTC clients
4. Collects gamepad/keyboard/mouse input from clients
5. Routes input back to Sunshine

## Requirements

- [Sunshine](https://github.com/LizardByte/Sunshine) running on the gaming PC
- Go 1.22+ (for building)
- Modern web browser with WebRTC support

## Quick Start

### 1. Build

```bash
go build -o gamelight ./cmd/gamelight
```

### 2. Configure Sunshine

Make sure Sunshine is running and accessible. Default ports:
- HTTP API: 47989
- HTTPS API: 47984
- Video: 47998
- Audio: 48000

### 3. Run Gamelight

```bash
./gamelight --sunshine-host <sunshine-ip>
```

### 4. Open in Browser

Navigate to `http://localhost:8080`

## Usage

### Session Flow

1. **First visitor** connects and becomes **Player 1 (Host)**
   - Full keyboard/mouse control
   - Gamepad mapped to slot 0
   - Can manage other players' permissions

2. **Additional visitors** join as **Spectators**
   - View-only access
   - Click "Join as Player" to get a player slot (2-4)

3. **Players 2-4**
   - Gamepad mapped to their slot (1, 2, or 3)
   - Keyboard/mouse access controlled by Host

### Controls

- **Fullscreen**: Double-click video or press F11
- **Capture Mouse**: Click on the video
- **Release Mouse**: Press Escape
- **Gamepad**: Connect any standard gamepad

## Configuration

Edit `config.yaml`:

```yaml
sunshine:
  host: "192.168.1.100"  # Sunshine server IP
  http_port: 47989
  https_port: 47984

webrtc:
  ice_servers:
    - urls: ["stun:stun.l.google.com:19302"]
    # Add TURN server for better NAT traversal:
    # - urls: ["turn:your-turn.com:3478"]
    #   username: "user"
    #   credential: "pass"

server:
  bind_address: "0.0.0.0:8080"
  # Enable HTTPS (required for Gamepad API):
  # tls_cert: "./certs/server.crt"
  # tls_key: "./certs/server.key"

stream:
  default_app: "Desktop"
  default_bitrate: 10000
  default_fps: 60
  default_width: 1920
  default_height: 1080
```

## HTTPS Setup

For the Gamepad API to work, browsers require a secure context (HTTPS). You can either:

1. **Use a reverse proxy** (nginx, Caddy) with SSL termination
2. **Configure Gamelight with certificates**:

```yaml
server:
  tls_cert: "./certs/server.crt"
  tls_key: "./certs/server.key"
```

Generate self-signed certificates:
```bash
openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
  -keyout certs/server.key -out certs/server.crt
```

## API

### WebSocket: `/ws`

WebRTC signaling and session management.

### REST: `GET /api/session`

Returns current session state.

## Project Structure

```
gamelight/
├── cmd/gamelight/      # Main entry point
├── internal/config/    # Configuration
├── pkg/
│   ├── sunshine/       # Sunshine/Moonlight protocol client
│   ├── rtsp/           # RTSP/RTP receiver
│   ├── webrtc/         # Pion WebRTC fan-out
│   ├── session/        # Session and player management
│   ├── input/          # Input handling
│   └── web/            # HTTP server and WebSocket
└── web/static/         # Frontend files
```

## Limitations

- Currently requires Sunshine to be pre-paired (automatic pairing coming soon)
- Input forwarding uses logging only (full control protocol integration in progress)
- Single session at a time

## Acknowledgements

- [Moonlight](https://moonlight-stream.org/) - Original game streaming client
- [Sunshine](https://github.com/LizardByte/Sunshine) - Game streaming host
- [moonlight-web-stream](https://github.com/MrCreativ3001/moonlight-web-stream) - Reference implementation
- [Pion WebRTC](https://github.com/pion/webrtc) - Go WebRTC library

## License

MIT License
