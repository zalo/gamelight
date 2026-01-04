package web

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"

	"github.com/gamelight/gamelight/internal/config"
	"github.com/gamelight/gamelight/pkg/input"
	"github.com/gamelight/gamelight/pkg/session"
	rtcfanout "github.com/gamelight/gamelight/pkg/webrtc"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for development
	},
}

// Server is the HTTP/WebSocket server
type Server struct {
	config         *config.Config
	sessionManager *session.Manager
	fanOut         *rtcfanout.FanOut
	inputHandler   *input.Handler

	clients   map[string]*Client
	clientsMu sync.RWMutex

	// Callbacks
	onStartStream func(settings session.StreamSettings) error
	onStopStream  func()
}

// Client represents a connected WebSocket client
type Client struct {
	ID       string
	Conn     *websocket.Conn
	send     chan []byte
	server   *Server
	peer     *rtcfanout.Peer
	mu       sync.Mutex
}

// Message types for WebSocket communication
type WSMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

type SDPMessage struct {
	SDP string `json:"sdp"`
}

type ICEMessage struct {
	Candidate        string  `json:"candidate"`
	SDPMid           *string `json:"sdpMid,omitempty"`
	SDPMLineIndex    *uint16 `json:"sdpMLineIndex,omitempty"`
	UsernameFragment *string `json:"usernameFragment,omitempty"`
}

type QualityMessage struct {
	Bitrate int `json:"bitrate"`
	FPS     int `json:"fps"`
	Width   int `json:"width"`
	Height  int `json:"height"`
}

type PermissionMessage struct {
	TargetID string `json:"target_id"`
	Keyboard bool   `json:"keyboard"`
	Mouse    bool   `json:"mouse"`
}

type SessionStateMessage struct {
	Participant *session.Participant `json:"you"`
	Session     session.State        `json:"session"`
}

// NewServer creates a new HTTP server
func NewServer(cfg *config.Config) (*Server, error) {
	fanOut, err := rtcfanout.NewFanOut(&cfg.WebRTC)
	if err != nil {
		return nil, err
	}

	s := &Server{
		config:         cfg,
		sessionManager: session.NewManager(),
		fanOut:         fanOut,
		inputHandler:   input.NewHandler(),
		clients:        make(map[string]*Client),
	}

	// Handle data channel messages
	fanOut.OnDataMessage(s.handleDataMessage)

	return s, nil
}

// OnStartStream sets the callback for when a stream should start
func (s *Server) OnStartStream(fn func(settings session.StreamSettings) error) {
	s.onStartStream = fn
}

// OnStopStream sets the callback for when a stream should stop
func (s *Server) OnStopStream(fn func()) {
	s.onStopStream = fn
}

// SetVideoTrack sets the video track for streaming
func (s *Server) SetVideoTrack(track *webrtc.TrackLocalStaticRTP) {
	s.fanOut.SetVideoTrack(track)
}

// SetAudioTrack sets the audio track for streaming
func (s *Server) SetAudioTrack(track *webrtc.TrackLocalStaticRTP) {
	s.fanOut.SetAudioTrack(track)
}

// InputHandler returns the input handler
func (s *Server) InputHandler() *input.Handler {
	return s.inputHandler
}

// SessionManager returns the session manager
func (s *Server) SessionManager() *session.Manager {
	return s.sessionManager
}

// Router returns the HTTP router
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"*"},
		AllowCredentials: true,
	}))

	// API routes
	r.Get("/api/session", s.handleGetSession)
	r.Get("/ws", s.handleWebSocket)

	// Serve static files
	staticDir := http.Dir("./web/static")
	fileServer := http.FileServer(staticDir)
	r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the file
		if r.URL.Path == "/" {
			r.URL.Path = "/index.html"
		}
		fileServer.ServeHTTP(w, r)
	})

	return r
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionManager.GetSession()

	var state session.State
	if sess != nil {
		state = sess.GetState()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(state)
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	clientID := uuid.New().String()
	client := &Client{
		ID:     clientID,
		Conn:   conn,
		send:   make(chan []byte, 256),
		server: s,
	}

	s.clientsMu.Lock()
	s.clients[clientID] = client
	s.clientsMu.Unlock()

	// Start client goroutines
	go client.writePump()
	go client.readPump()

	// Join session or create one
	s.handleClientJoin(client)
}

func (s *Server) handleClientJoin(client *Client) {
	sess := s.sessionManager.GetSession()

	// Create session if none exists
	if sess == nil {
		settings := session.StreamSettings{
			Bitrate: s.config.Stream.DefaultBitrate,
			FPS:     s.config.Stream.DefaultFPS,
			Width:   s.config.Stream.DefaultWidth,
			Height:  s.config.Stream.DefaultHeight,
		}

		var err error
		sess, err = s.sessionManager.CreateSession(0, s.config.Stream.DefaultApp, settings)
		if err != nil {
			log.Printf("Failed to create session: %v", err)
			return
		}

		// Start streaming
		if s.onStartStream != nil {
			if err := s.onStartStream(settings); err != nil {
				log.Printf("Failed to start stream: %v", err)
				s.sessionManager.EndSession()
				return
			}
		}
	}

	// Add participant to session
	participant := sess.Join(client.ID, "Player")

	// Send initial state
	s.sendSessionState(client, sess, participant)
}

func (s *Server) handleClientLeave(clientID string) {
	sess := s.sessionManager.GetSession()
	if sess == nil {
		return
	}

	_, sessionEnded := sess.Leave(clientID)

	if sessionEnded {
		if s.onStopStream != nil {
			s.onStopStream()
		}
		s.sessionManager.EndSession()
	}

	// Broadcast updated state
	s.broadcastSessionState()
}

func (s *Server) sendSessionState(client *Client, sess *session.Session, participant *session.Participant) {
	state := SessionStateMessage{
		Participant: participant,
		Session:     sess.GetState(),
	}

	data, _ := json.Marshal(state)
	msg := WSMessage{Type: "session_state", Data: data}
	msgBytes, _ := json.Marshal(msg)

	select {
	case client.send <- msgBytes:
	default:
	}
}

func (s *Server) broadcastSessionState() {
	sess := s.sessionManager.GetSession()
	if sess == nil {
		return
	}

	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	for _, client := range s.clients {
		participant := sess.GetParticipant(client.ID)
		if participant != nil {
			s.sendSessionState(client, sess, participant)
		}
	}
}

func (s *Server) handleDataMessage(peerID string, channel string, data []byte) {
	sess := s.sessionManager.GetSession()
	if sess == nil {
		return
	}

	switch channel {
	case "mouse_relative", "mouse_move":
		if !sess.CanUseMouse(peerID) {
			return
		}
		if event, err := input.ParseMouseMoveData(data); err == nil && event != nil {
			s.inputHandler.HandleMouseMove(event.DeltaX, event.DeltaY)
		}

	case "mouse_absolute", "mouse_position":
		if !sess.CanUseMouse(peerID) {
			return
		}
		if event, err := input.ParseMousePositionData(data); err == nil && event != nil {
			s.inputHandler.HandleMousePosition(event.X, event.Y, event.Width, event.Height)
		}

	case "mouse_button":
		if !sess.CanUseMouse(peerID) {
			return
		}
		if event, err := input.ParseMouseButtonData(data); err == nil && event != nil {
			s.inputHandler.HandleMouseButton(event.Button, event.Action)
		}

	case "mouse_scroll":
		if !sess.CanUseMouse(peerID) {
			return
		}
		if event, err := input.ParseMouseScrollData(data); err == nil && event != nil {
			s.inputHandler.HandleMouseScroll(event.Amount)
		}

	case "keyboard":
		if !sess.CanUseKeyboard(peerID) {
			return
		}
		if event, err := input.ParseKeyboardData(data); err == nil && event != nil {
			s.inputHandler.HandleKeyboard(event.KeyCode, event.Action, event.Modifiers)
		}

	case "controllers", "controller0", "controller1", "controller2", "controller3":
		slot := sess.GetSlotByID(peerID)
		if slot == session.SlotNone {
			return // Spectators can't send controller input
		}
		if event, err := input.ParseControllerData(data); err == nil && event != nil {
			// Override controller number with player's slot
			event.ControllerNumber = uint8(slot - 1) // Slots are 1-4, controllers are 0-3
			s.inputHandler.HandleController(*event)
		}
	}
}

// Client methods

func (c *Client) readPump() {
	defer func() {
		c.server.handleClientLeave(c.ID)
		c.server.fanOut.RemovePeer(c.ID)
		c.server.clientsMu.Lock()
		delete(c.server.clients, c.ID)
		c.server.clientsMu.Unlock()
		c.Conn.Close()
	}()

	for {
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		var msg WSMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			log.Printf("Invalid message: %v", err)
			continue
		}

		c.handleMessage(msg)
	}
}

func (c *Client) writePump() {
	defer c.Conn.Close()

	for message := range c.send {
		if err := c.Conn.WriteMessage(websocket.TextMessage, message); err != nil {
			return
		}
	}
}

func (c *Client) handleMessage(msg WSMessage) {
	switch msg.Type {
	case "offer":
		var sdp SDPMessage
		if err := json.Unmarshal(msg.Data, &sdp); err != nil {
			log.Printf("Invalid offer: %v", err)
			return
		}
		c.handleOffer(sdp)

	case "ice_candidate":
		var ice ICEMessage
		if err := json.Unmarshal(msg.Data, &ice); err != nil {
			log.Printf("Invalid ICE candidate: %v", err)
			return
		}
		c.handleICECandidate(ice)

	case "join_as_player":
		c.handleJoinAsPlayer()

	case "spectate":
		c.handleSpectate()

	case "set_quality":
		var quality QualityMessage
		if err := json.Unmarshal(msg.Data, &quality); err != nil {
			return
		}
		c.handleSetQuality(quality)

	case "set_permission":
		var perm PermissionMessage
		if err := json.Unmarshal(msg.Data, &perm); err != nil {
			return
		}
		c.handleSetPermission(perm)
	}
}

func (c *Client) handleOffer(sdp SDPMessage) {
	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  sdp.SDP,
	}

	answer, err := c.server.fanOut.HandleOffer(c.ID, offer)
	if err != nil {
		log.Printf("Failed to handle offer: %v", err)
		return
	}

	// Get the peer and set up ICE candidate handler
	peer := c.server.fanOut.GetPeer(c.ID)
	if peer != nil {
		c.peer = peer
		peer.OnICECandidate(func(candidate *webrtc.ICECandidate) {
			if candidate == nil {
				return
			}
			c.sendICECandidate(candidate)
		})
	}

	// Send answer
	answerData, _ := json.Marshal(SDPMessage{SDP: answer.SDP})
	msg := WSMessage{Type: "answer", Data: answerData}
	msgBytes, _ := json.Marshal(msg)

	select {
	case c.send <- msgBytes:
	default:
	}
}

func (c *Client) handleICECandidate(ice ICEMessage) {
	candidate := webrtc.ICECandidateInit{
		Candidate:        ice.Candidate,
		SDPMid:           ice.SDPMid,
		SDPMLineIndex:    ice.SDPMLineIndex,
		UsernameFragment: ice.UsernameFragment,
	}

	if err := c.server.fanOut.AddICECandidate(c.ID, candidate); err != nil {
		log.Printf("Failed to add ICE candidate: %v", err)
	}
}

func (c *Client) sendICECandidate(candidate *webrtc.ICECandidate) {
	json := candidate.ToJSON()
	ice := ICEMessage{
		Candidate:        json.Candidate,
		SDPMid:           json.SDPMid,
		SDPMLineIndex:    json.SDPMLineIndex,
		UsernameFragment: json.UsernameFragment,
	}

	iceData, _ := json.Marshal(ice)
	msg := WSMessage{Type: "ice_candidate", Data: iceData}
	msgBytes, _ := json.Marshal(msg)

	select {
	case c.send <- msgBytes:
	default:
	}
}

func (c *Client) handleJoinAsPlayer() {
	sess := c.server.sessionManager.GetSession()
	if sess == nil {
		return
	}

	if err := sess.JoinAsPlayer(c.ID); err != nil {
		log.Printf("Failed to join as player: %v", err)
		return
	}

	c.server.broadcastSessionState()
}

func (c *Client) handleSpectate() {
	sess := c.server.sessionManager.GetSession()
	if sess == nil {
		return
	}

	if err := sess.Spectate(c.ID); err != nil {
		log.Printf("Failed to spectate: %v", err)
		return
	}

	c.server.broadcastSessionState()
}

func (c *Client) handleSetQuality(quality QualityMessage) {
	sess := c.server.sessionManager.GetSession()
	if sess == nil || !sess.IsHost(c.ID) {
		return
	}

	// Update stream quality (would need to restart stream)
	// For now just log
	log.Printf("Quality change requested: %+v", quality)
}

func (c *Client) handleSetPermission(perm PermissionMessage) {
	sess := c.server.sessionManager.GetSession()
	if sess == nil {
		return
	}

	if perm.Keyboard {
		sess.SetKeyboardPermission(c.ID, perm.TargetID, true)
	} else {
		sess.SetKeyboardPermission(c.ID, perm.TargetID, false)
	}

	if perm.Mouse {
		sess.SetMousePermission(c.ID, perm.TargetID, true)
	} else {
		sess.SetMousePermission(c.ID, perm.TargetID, false)
	}

	c.server.broadcastSessionState()
}

func (c *Client) sendJSON(msgType string, v interface{}) {
	data, _ := json.Marshal(v)
	msg := WSMessage{Type: msgType, Data: data}
	msgBytes, _ := json.Marshal(msg)

	select {
	case c.send <- msgBytes:
	default:
	}
}
