package webrtc

import (
	"errors"
	"log"
	"sync"

	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/intervalpli"
	"github.com/pion/webrtc/v4"

	"github.com/gamelight/gamelight/internal/config"
)

var (
	ErrNoVideoTrack = errors.New("no video track available")
	ErrNoAudioTrack = errors.New("no audio track available")
)

// FanOut manages multiple WebRTC peer connections sharing the same media source
type FanOut struct {
	mu sync.RWMutex

	api    *webrtc.API
	config webrtc.Configuration

	// Source tracks from Sunshine
	videoTrack *webrtc.TrackLocalStaticRTP
	audioTrack *webrtc.TrackLocalStaticRTP

	// Connected peers
	peers map[string]*Peer

	// Callbacks
	onDataMessage func(peerID string, channel string, data []byte)
}

// Peer represents a connected WebRTC peer
type Peer struct {
	ID         string
	Connection *webrtc.PeerConnection

	videoSender *webrtc.RTPSender
	audioSender *webrtc.RTPSender

	dataChannels map[string]*webrtc.DataChannel
	mu           sync.RWMutex
}

// NewFanOut creates a new WebRTC fan-out manager
func NewFanOut(cfg *config.WebRTCConfig) (*FanOut, error) {
	// Create media engine
	m := &webrtc.MediaEngine{}

	// Register default codecs
	if err := m.RegisterDefaultCodecs(); err != nil {
		return nil, err
	}

	// Create interceptor registry
	i := &interceptor.Registry{}

	// Add PLI interceptor for keyframe requests
	intervalPliFactory, err := intervalpli.NewReceiverInterceptor()
	if err != nil {
		return nil, err
	}
	i.Add(intervalPliFactory)

	// Use default interceptors
	if err := webrtc.RegisterDefaultInterceptors(m, i); err != nil {
		return nil, err
	}

	// Create setting engine for port range
	s := webrtc.SettingEngine{}
	if cfg.PortRange != nil {
		s.SetEphemeralUDPPortRange(cfg.PortRange.Min, cfg.PortRange.Max)
	}

	// Build API
	api := webrtc.NewAPI(
		webrtc.WithMediaEngine(m),
		webrtc.WithInterceptorRegistry(i),
		webrtc.WithSettingEngine(s),
	)

	// Convert ICE servers
	iceServers := make([]webrtc.ICEServer, 0, len(cfg.ICEServers))
	for _, server := range cfg.ICEServers {
		ice := webrtc.ICEServer{
			URLs: server.URLs,
		}
		if server.Username != "" {
			ice.Username = server.Username
			ice.Credential = server.Credential
			ice.CredentialType = webrtc.ICECredentialTypePassword
		}
		iceServers = append(iceServers, ice)
	}

	return &FanOut{
		api: api,
		config: webrtc.Configuration{
			ICEServers: iceServers,
		},
		peers: make(map[string]*Peer),
	}, nil
}

// SetVideoTrack sets the video track that will be fanned out to all peers
func (f *FanOut) SetVideoTrack(track *webrtc.TrackLocalStaticRTP) {
	f.mu.Lock()
	f.videoTrack = track
	f.mu.Unlock()

	// Add to existing peers
	f.mu.RLock()
	defer f.mu.RUnlock()

	for _, peer := range f.peers {
		if peer.videoSender == nil {
			sender, err := peer.Connection.AddTrack(track)
			if err != nil {
				log.Printf("Error adding video track to peer %s: %v", peer.ID, err)
				continue
			}
			peer.videoSender = sender

			// Handle RTCP
			go f.handleRTCP(sender)
		}
	}
}

// SetAudioTrack sets the audio track that will be fanned out to all peers
func (f *FanOut) SetAudioTrack(track *webrtc.TrackLocalStaticRTP) {
	f.mu.Lock()
	f.audioTrack = track
	f.mu.Unlock()

	// Add to existing peers
	f.mu.RLock()
	defer f.mu.RUnlock()

	for _, peer := range f.peers {
		if peer.audioSender == nil {
			sender, err := peer.Connection.AddTrack(track)
			if err != nil {
				log.Printf("Error adding audio track to peer %s: %v", peer.ID, err)
				continue
			}
			peer.audioSender = sender

			// Handle RTCP
			go f.handleRTCP(sender)
		}
	}
}

// OnDataMessage sets the callback for incoming data channel messages
func (f *FanOut) OnDataMessage(fn func(peerID string, channel string, data []byte)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.onDataMessage = fn
}

// AddPeer creates a new peer connection
func (f *FanOut) AddPeer(id string) (*Peer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Create peer connection
	pc, err := f.api.NewPeerConnection(f.config)
	if err != nil {
		return nil, err
	}

	peer := &Peer{
		ID:           id,
		Connection:   pc,
		dataChannels: make(map[string]*webrtc.DataChannel),
	}

	// Add video track if available
	if f.videoTrack != nil {
		sender, err := pc.AddTrack(f.videoTrack)
		if err != nil {
			pc.Close()
			return nil, err
		}
		peer.videoSender = sender
		go f.handleRTCP(sender)
	}

	// Add audio track if available
	if f.audioTrack != nil {
		sender, err := pc.AddTrack(f.audioTrack)
		if err != nil {
			pc.Close()
			return nil, err
		}
		peer.audioSender = sender
		go f.handleRTCP(sender)
	}

	// Handle incoming data channels
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		peer.mu.Lock()
		peer.dataChannels[dc.Label()] = dc
		peer.mu.Unlock()

		dc.OnMessage(func(msg webrtc.DataChannelMessage) {
			f.mu.RLock()
			fn := f.onDataMessage
			f.mu.RUnlock()

			if fn != nil {
				fn(id, dc.Label(), msg.Data)
			}
		})
	})

	// Handle connection state changes
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("Peer %s connection state: %s", id, state)

		if state == webrtc.PeerConnectionStateFailed ||
			state == webrtc.PeerConnectionStateClosed ||
			state == webrtc.PeerConnectionStateDisconnected {
			f.RemovePeer(id)
		}
	})

	f.peers[id] = peer
	return peer, nil
}

// RemovePeer removes a peer connection
func (f *FanOut) RemovePeer(id string) {
	f.mu.Lock()
	peer, exists := f.peers[id]
	if exists {
		delete(f.peers, id)
	}
	f.mu.Unlock()

	if peer != nil {
		peer.Connection.Close()
	}
}

// GetPeer returns a peer by ID
func (f *FanOut) GetPeer(id string) *Peer {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.peers[id]
}

// GetPeerCount returns the number of connected peers
func (f *FanOut) GetPeerCount() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.peers)
}

// HandleOffer processes an SDP offer from a peer and returns an answer
func (f *FanOut) HandleOffer(peerID string, offer webrtc.SessionDescription) (*webrtc.SessionDescription, error) {
	peer := f.GetPeer(peerID)
	if peer == nil {
		var err error
		peer, err = f.AddPeer(peerID)
		if err != nil {
			return nil, err
		}
	}

	if err := peer.Connection.SetRemoteDescription(offer); err != nil {
		return nil, err
	}

	answer, err := peer.Connection.CreateAnswer(nil)
	if err != nil {
		return nil, err
	}

	if err := peer.Connection.SetLocalDescription(answer); err != nil {
		return nil, err
	}

	return &answer, nil
}

// AddICECandidate adds an ICE candidate to a peer
func (f *FanOut) AddICECandidate(peerID string, candidate webrtc.ICECandidateInit) error {
	peer := f.GetPeer(peerID)
	if peer == nil {
		return errors.New("peer not found")
	}

	return peer.Connection.AddICECandidate(candidate)
}

// GetLocalDescription returns the local SDP for a peer
func (f *FanOut) GetLocalDescription(peerID string) *webrtc.SessionDescription {
	peer := f.GetPeer(peerID)
	if peer == nil {
		return nil
	}

	return peer.Connection.LocalDescription()
}

// OnICECandidate sets a callback for when a new ICE candidate is generated
func (p *Peer) OnICECandidate(fn func(*webrtc.ICECandidate)) {
	p.Connection.OnICECandidate(fn)
}

// CreateDataChannel creates a data channel for sending data to the peer
func (p *Peer) CreateDataChannel(label string) (*webrtc.DataChannel, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if dc, exists := p.dataChannels[label]; exists {
		return dc, nil
	}

	dc, err := p.Connection.CreateDataChannel(label, nil)
	if err != nil {
		return nil, err
	}

	p.dataChannels[label] = dc
	return dc, nil
}

// SendDataChannel sends data over a specific data channel
func (p *Peer) SendDataChannel(label string, data []byte) error {
	p.mu.RLock()
	dc, exists := p.dataChannels[label]
	p.mu.RUnlock()

	if !exists {
		return errors.New("data channel not found")
	}

	return dc.Send(data)
}

// handleRTCP handles RTCP packets from receivers
func (f *FanOut) handleRTCP(sender *webrtc.RTPSender) {
	rtcpBuf := make([]byte, 1500)
	for {
		if _, _, err := sender.Read(rtcpBuf); err != nil {
			return
		}
		// RTCP packets are handled automatically by pion
		// This goroutine just needs to drain the channel
	}
}

// WriteRTP writes an RTP packet to all peers via the video track
func (f *FanOut) WriteVideoRTP(payload []byte) error {
	f.mu.RLock()
	track := f.videoTrack
	f.mu.RUnlock()

	if track == nil {
		return ErrNoVideoTrack
	}

	_, err := track.Write(payload)
	return err
}

// WriteAudioRTP writes an RTP packet to all peers via the audio track
func (f *FanOut) WriteAudioRTP(payload []byte) error {
	f.mu.RLock()
	track := f.audioTrack
	f.mu.RUnlock()

	if track == nil {
		return ErrNoAudioTrack
	}

	_, err := track.Write(payload)
	return err
}

// Close closes all peer connections
func (f *FanOut) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, peer := range f.peers {
		peer.Connection.Close()
	}
	f.peers = make(map[string]*Peer)
}

// CreateVideoTrack creates a new video track for the given codec
func CreateVideoTrack(codecMimeType string) (*webrtc.TrackLocalStaticRTP, error) {
	return webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: codecMimeType},
		"video",
		"gamelight-video",
	)
}

// CreateAudioTrack creates a new audio track
func CreateAudioTrack() (*webrtc.TrackLocalStaticRTP, error) {
	return webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"audio",
		"gamelight-audio",
	)
}
