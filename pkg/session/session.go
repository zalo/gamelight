package session

import (
	"errors"
	"sync"

	"github.com/google/uuid"
)

var (
	ErrNoSlotAvailable = errors.New("no player slot available")
	ErrAlreadyPlayer   = errors.New("already a player")
	ErrNotAPlayer      = errors.New("not a player")
	ErrNotHost         = errors.New("only host can perform this action")
	ErrSessionExists   = errors.New("session already exists")
	ErrNoSession       = errors.New("no active session")
)

// PlayerSlot represents a player slot (1-4)
type PlayerSlot int

const (
	SlotNone PlayerSlot = 0
	Slot1    PlayerSlot = 1
	Slot2    PlayerSlot = 2
	Slot3    PlayerSlot = 3
	Slot4    PlayerSlot = 4
)

// Role represents the participant's role
type Role string

const (
	RoleSpectator Role = "spectator"
	RolePlayer    Role = "player"
)

// Participant represents someone connected to the session
type Participant struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Role         Role       `json:"role"`
	Slot         PlayerSlot `json:"slot,omitempty"`
	IsHost       bool       `json:"is_host"`
	CanKeyboard  bool       `json:"can_keyboard"`  // Can use keyboard
	CanMouse     bool       `json:"can_mouse"`     // Can use mouse
}

// StreamSettings holds the current stream quality settings
type StreamSettings struct {
	Bitrate int `json:"bitrate"`
	FPS     int `json:"fps"`
	Width   int `json:"width"`
	Height  int `json:"height"`
}

// Session represents an active streaming session
type Session struct {
	mu sync.RWMutex

	ID           string
	AppID        int
	AppName      string
	Settings     StreamSettings
	participants map[string]*Participant
	slots        [5]*Participant // Index 0 unused, slots 1-4
	hostID       string

	// Callbacks
	onParticipantJoin   func(*Participant)
	onParticipantLeave  func(*Participant)
	onParticipantUpdate func(*Participant)
}

// Manager manages streaming sessions
type Manager struct {
	mu      sync.RWMutex
	session *Session
}

// NewManager creates a new session manager
func NewManager() *Manager {
	return &Manager{}
}

// CreateSession creates a new streaming session
func (m *Manager) CreateSession(appID int, appName string, settings StreamSettings) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.session != nil {
		return nil, ErrSessionExists
	}

	m.session = &Session{
		ID:           uuid.New().String()[:8],
		AppID:        appID,
		AppName:      appName,
		Settings:     settings,
		participants: make(map[string]*Participant),
	}

	return m.session, nil
}

// GetSession returns the current session
func (m *Manager) GetSession() *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.session
}

// EndSession ends the current session
func (m *Manager) EndSession() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.session = nil
}

// Join adds a participant to the session
func (s *Session) Join(id, name string) *Participant {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if already in session
	if p, exists := s.participants[id]; exists {
		return p
	}

	// First participant becomes host with slot 1
	isHost := len(s.participants) == 0
	role := RoleSpectator
	slot := SlotNone

	if isHost {
		role = RolePlayer
		slot = Slot1
	}

	p := &Participant{
		ID:          id,
		Name:        name,
		Role:        role,
		Slot:        slot,
		IsHost:      isHost,
		CanKeyboard: isHost,
		CanMouse:    isHost,
	}

	s.participants[id] = p
	if slot != SlotNone {
		s.slots[slot] = p
	}
	if isHost {
		s.hostID = id
	}

	if s.onParticipantJoin != nil {
		s.onParticipantJoin(p)
	}

	return p
}

// Leave removes a participant from the session
func (s *Session) Leave(id string) (*Participant, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, exists := s.participants[id]
	if !exists {
		return nil, false
	}

	// Clear slot
	if p.Slot != SlotNone {
		s.slots[p.Slot] = nil
	}

	delete(s.participants, id)

	if s.onParticipantLeave != nil {
		s.onParticipantLeave(p)
	}

	// If host left, transfer to next player or end session
	wasHost := p.IsHost
	if wasHost {
		s.hostID = ""
		// Find next player to become host
		for _, participant := range s.participants {
			if participant.Role == RolePlayer {
				participant.IsHost = true
				participant.CanKeyboard = true
				participant.CanMouse = true
				s.hostID = participant.ID
				if s.onParticipantUpdate != nil {
					s.onParticipantUpdate(participant)
				}
				break
			}
		}
	}

	return p, wasHost && s.hostID == ""
}

// JoinAsPlayer promotes a spectator to a player
func (s *Session) JoinAsPlayer(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, exists := s.participants[id]
	if !exists {
		return ErrNoSession
	}

	if p.Role == RolePlayer {
		return ErrAlreadyPlayer
	}

	// Find available slot
	slot := SlotNone
	for i := Slot1; i <= Slot4; i++ {
		if s.slots[i] == nil {
			slot = i
			break
		}
	}

	if slot == SlotNone {
		return ErrNoSlotAvailable
	}

	p.Role = RolePlayer
	p.Slot = slot
	s.slots[slot] = p

	if s.onParticipantUpdate != nil {
		s.onParticipantUpdate(p)
	}

	return nil
}

// Spectate demotes a player to spectator
func (s *Session) Spectate(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, exists := s.participants[id]
	if !exists {
		return ErrNoSession
	}

	if p.Role != RolePlayer {
		return ErrNotAPlayer
	}

	// Host cannot spectate
	if p.IsHost {
		return ErrNotHost
	}

	// Clear slot
	if p.Slot != SlotNone {
		s.slots[p.Slot] = nil
	}

	p.Role = RoleSpectator
	p.Slot = SlotNone
	p.CanKeyboard = false
	p.CanMouse = false

	if s.onParticipantUpdate != nil {
		s.onParticipantUpdate(p)
	}

	return nil
}

// SetKeyboardPermission sets keyboard permission for a player (host only)
func (s *Session) SetKeyboardPermission(hostID, targetID string, allowed bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.hostID != hostID {
		return ErrNotHost
	}

	p, exists := s.participants[targetID]
	if !exists {
		return ErrNoSession
	}

	p.CanKeyboard = allowed

	if s.onParticipantUpdate != nil {
		s.onParticipantUpdate(p)
	}

	return nil
}

// SetMousePermission sets mouse permission for a player (host only)
func (s *Session) SetMousePermission(hostID, targetID string, allowed bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.hostID != hostID {
		return ErrNotHost
	}

	p, exists := s.participants[targetID]
	if !exists {
		return ErrNoSession
	}

	p.CanMouse = allowed

	if s.onParticipantUpdate != nil {
		s.onParticipantUpdate(p)
	}

	return nil
}

// GetParticipant returns a participant by ID
func (s *Session) GetParticipant(id string) *Participant {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.participants[id]
}

// GetParticipants returns all participants
func (s *Session) GetParticipants() []*Participant {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*Participant, 0, len(s.participants))
	for _, p := range s.participants {
		result = append(result, p)
	}
	return result
}

// GetPlayers returns only players (not spectators)
func (s *Session) GetPlayers() []*Participant {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*Participant, 0, 4)
	for i := Slot1; i <= Slot4; i++ {
		if s.slots[i] != nil {
			result = append(result, s.slots[i])
		}
	}
	return result
}

// GetSpectatorCount returns the number of spectators
func (s *Session) GetSpectatorCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, p := range s.participants {
		if p.Role == RoleSpectator {
			count++
		}
	}
	return count
}

// GetHost returns the host participant
func (s *Session) GetHost() *Participant {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.hostID == "" {
		return nil
	}
	return s.participants[s.hostID]
}

// IsHost checks if the given ID is the host
func (s *Session) IsHost(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hostID == id
}

// GetSlotByID returns the slot for a participant
func (s *Session) GetSlotByID(id string) PlayerSlot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if p, exists := s.participants[id]; exists {
		return p.Slot
	}
	return SlotNone
}

// CanUseKeyboard checks if a participant can use the keyboard
func (s *Session) CanUseKeyboard(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if p, exists := s.participants[id]; exists {
		return p.CanKeyboard
	}
	return false
}

// CanUseMouse checks if a participant can use the mouse
func (s *Session) CanUseMouse(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if p, exists := s.participants[id]; exists {
		return p.CanMouse
	}
	return false
}

// GetActiveGamepads returns a bitmask of active gamepad slots
func (s *Session) GetActiveGamepads() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	mask := 0
	for i := Slot1; i <= Slot4; i++ {
		if s.slots[i] != nil {
			mask |= 1 << (i - 1)
		}
	}
	return mask
}

// OnParticipantJoin sets the callback for when a participant joins
func (s *Session) OnParticipantJoin(fn func(*Participant)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onParticipantJoin = fn
}

// OnParticipantLeave sets the callback for when a participant leaves
func (s *Session) OnParticipantLeave(fn func(*Participant)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onParticipantLeave = fn
}

// OnParticipantUpdate sets the callback for when a participant is updated
func (s *Session) OnParticipantUpdate(fn func(*Participant)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onParticipantUpdate = fn
}

// State returns the current session state for API responses
type State struct {
	Active      bool            `json:"active"`
	ID          string          `json:"id,omitempty"`
	AppName     string          `json:"app_name,omitempty"`
	Players     []*Participant  `json:"players,omitempty"`
	Spectators  int             `json:"spectators,omitempty"`
	Settings    *StreamSettings `json:"settings,omitempty"`
}

// GetState returns the current session state
func (s *Session) GetState() State {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return State{
		Active:     true,
		ID:         s.ID,
		AppName:    s.AppName,
		Players:    s.GetPlayers(),
		Spectators: s.GetSpectatorCount(),
		Settings:   &s.Settings,
	}
}
