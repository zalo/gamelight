package input

import (
	"encoding/binary"
	"sync"
)

// EventType represents the type of input event
type EventType uint8

const (
	EventTypeMouseMove EventType = iota
	EventTypeMouseButton
	EventTypeMouseScroll
	EventTypeKeyboard
	EventTypeController
	EventTypeTouch
)

// MouseButton represents mouse buttons
type MouseButton uint8

const (
	MouseButtonLeft   MouseButton = 1
	MouseButtonMiddle MouseButton = 2
	MouseButtonRight  MouseButton = 3
	MouseButton4      MouseButton = 4
	MouseButton5      MouseButton = 5
)

// MouseButtonAction represents mouse button actions
type MouseButtonAction uint8

const (
	MouseButtonDown MouseButtonAction = 0x07
	MouseButtonUp   MouseButtonAction = 0x08
)

// KeyAction represents keyboard key actions
type KeyAction uint8

const (
	KeyDown KeyAction = 0x03
	KeyUp   KeyAction = 0x04
)

// ControllerButton represents controller buttons (Xbox layout)
type ControllerButton uint32

const (
	ControllerButtonA            ControllerButton = 0x1000
	ControllerButtonB            ControllerButton = 0x2000
	ControllerButtonX            ControllerButton = 0x4000
	ControllerButtonY            ControllerButton = 0x8000
	ControllerButtonUp           ControllerButton = 0x0001
	ControllerButtonDown         ControllerButton = 0x0002
	ControllerButtonLeft         ControllerButton = 0x0004
	ControllerButtonRight        ControllerButton = 0x0008
	ControllerButtonStart        ControllerButton = 0x0010
	ControllerButtonBack         ControllerButton = 0x0020
	ControllerButtonLeftStick    ControllerButton = 0x0040
	ControllerButtonRightStick   ControllerButton = 0x0080
	ControllerButtonLeftBumper   ControllerButton = 0x0100
	ControllerButtonRightBumper  ControllerButton = 0x0200
	ControllerButtonGuide        ControllerButton = 0x0400
	ControllerButtonMisc         ControllerButton = 0x0800
	ControllerButtonPaddle1      ControllerButton = 0x010000
	ControllerButtonPaddle2      ControllerButton = 0x020000
	ControllerButtonPaddle3      ControllerButton = 0x040000
	ControllerButtonPaddle4      ControllerButton = 0x080000
	ControllerButtonTouchpad     ControllerButton = 0x100000
)

// MouseMoveEvent represents a mouse movement
type MouseMoveEvent struct {
	DeltaX int16
	DeltaY int16
}

// MousePositionEvent represents absolute mouse position
type MousePositionEvent struct {
	X      int16
	Y      int16
	Width  int16
	Height int16
}

// MouseButtonEvent represents a mouse button press/release
type MouseButtonEvent struct {
	Button MouseButton
	Action MouseButtonAction
}

// MouseScrollEvent represents mouse scroll
type MouseScrollEvent struct {
	Amount int16
}

// KeyboardEvent represents a keyboard key press/release
type KeyboardEvent struct {
	KeyCode   uint16
	Action    KeyAction
	Modifiers uint8
}

// ControllerEvent represents controller state
type ControllerEvent struct {
	ControllerNumber uint8
	Buttons          ControllerButton
	LeftTrigger      uint8
	RightTrigger     uint8
	LeftStickX       int16
	LeftStickY       int16
	RightStickX      int16
	RightStickY      int16
}

// Handler processes input events from clients
type Handler struct {
	mu sync.RWMutex

	// Callback for sending input to Sunshine
	onMouseMove     func(MouseMoveEvent)
	onMousePosition func(MousePositionEvent)
	onMouseButton   func(MouseButtonEvent)
	onMouseScroll   func(MouseScrollEvent)
	onKeyboard      func(KeyboardEvent)
	onController    func(ControllerEvent)
}

// NewHandler creates a new input handler
func NewHandler() *Handler {
	return &Handler{}
}

// OnMouseMove sets the callback for mouse movement events
func (h *Handler) OnMouseMove(fn func(MouseMoveEvent)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onMouseMove = fn
}

// OnMousePosition sets the callback for absolute mouse position events
func (h *Handler) OnMousePosition(fn func(MousePositionEvent)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onMousePosition = fn
}

// OnMouseButton sets the callback for mouse button events
func (h *Handler) OnMouseButton(fn func(MouseButtonEvent)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onMouseButton = fn
}

// OnMouseScroll sets the callback for mouse scroll events
func (h *Handler) OnMouseScroll(fn func(MouseScrollEvent)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onMouseScroll = fn
}

// OnKeyboard sets the callback for keyboard events
func (h *Handler) OnKeyboard(fn func(KeyboardEvent)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onKeyboard = fn
}

// OnController sets the callback for controller events
func (h *Handler) OnController(fn func(ControllerEvent)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onController = fn
}

// HandleMouseMove processes a mouse movement event
func (h *Handler) HandleMouseMove(deltaX, deltaY int16) {
	h.mu.RLock()
	fn := h.onMouseMove
	h.mu.RUnlock()

	if fn != nil {
		fn(MouseMoveEvent{DeltaX: deltaX, DeltaY: deltaY})
	}
}

// HandleMousePosition processes an absolute mouse position event
func (h *Handler) HandleMousePosition(x, y, width, height int16) {
	h.mu.RLock()
	fn := h.onMousePosition
	h.mu.RUnlock()

	if fn != nil {
		fn(MousePositionEvent{X: x, Y: y, Width: width, Height: height})
	}
}

// HandleMouseButton processes a mouse button event
func (h *Handler) HandleMouseButton(button MouseButton, action MouseButtonAction) {
	h.mu.RLock()
	fn := h.onMouseButton
	h.mu.RUnlock()

	if fn != nil {
		fn(MouseButtonEvent{Button: button, Action: action})
	}
}

// HandleMouseScroll processes a mouse scroll event
func (h *Handler) HandleMouseScroll(amount int16) {
	h.mu.RLock()
	fn := h.onMouseScroll
	h.mu.RUnlock()

	if fn != nil {
		fn(MouseScrollEvent{Amount: amount})
	}
}

// HandleKeyboard processes a keyboard event
func (h *Handler) HandleKeyboard(keyCode uint16, action KeyAction, modifiers uint8) {
	h.mu.RLock()
	fn := h.onKeyboard
	h.mu.RUnlock()

	if fn != nil {
		fn(KeyboardEvent{KeyCode: keyCode, Action: action, Modifiers: modifiers})
	}
}

// HandleController processes a controller event
func (h *Handler) HandleController(event ControllerEvent) {
	h.mu.RLock()
	fn := h.onController
	h.mu.RUnlock()

	if fn != nil {
		fn(event)
	}
}

// ParseControllerData parses binary controller data from the browser
func ParseControllerData(data []byte) (*ControllerEvent, error) {
	if len(data) < 13 {
		return nil, nil
	}

	return &ControllerEvent{
		ControllerNumber: data[0],
		Buttons:          ControllerButton(binary.LittleEndian.Uint32(data[1:5])),
		LeftTrigger:      data[5],
		RightTrigger:     data[6],
		LeftStickX:       int16(binary.LittleEndian.Uint16(data[7:9])),
		LeftStickY:       int16(binary.LittleEndian.Uint16(data[9:11])),
		RightStickX:      int16(binary.LittleEndian.Uint16(data[11:13])),
		RightStickY:      int16(binary.LittleEndian.Uint16(data[13:15])),
	}, nil
}

// ParseMouseMoveData parses binary mouse move data
func ParseMouseMoveData(data []byte) (*MouseMoveEvent, error) {
	if len(data) < 4 {
		return nil, nil
	}

	return &MouseMoveEvent{
		DeltaX: int16(binary.LittleEndian.Uint16(data[0:2])),
		DeltaY: int16(binary.LittleEndian.Uint16(data[2:4])),
	}, nil
}

// ParseMousePositionData parses binary mouse position data
func ParseMousePositionData(data []byte) (*MousePositionEvent, error) {
	if len(data) < 8 {
		return nil, nil
	}

	return &MousePositionEvent{
		X:      int16(binary.LittleEndian.Uint16(data[0:2])),
		Y:      int16(binary.LittleEndian.Uint16(data[2:4])),
		Width:  int16(binary.LittleEndian.Uint16(data[4:6])),
		Height: int16(binary.LittleEndian.Uint16(data[6:8])),
	}, nil
}

// ParseKeyboardData parses binary keyboard data
func ParseKeyboardData(data []byte) (*KeyboardEvent, error) {
	if len(data) < 4 {
		return nil, nil
	}

	return &KeyboardEvent{
		KeyCode:   binary.LittleEndian.Uint16(data[0:2]),
		Action:    KeyAction(data[2]),
		Modifiers: data[3],
	}, nil
}

// ParseMouseButtonData parses binary mouse button data
func ParseMouseButtonData(data []byte) (*MouseButtonEvent, error) {
	if len(data) < 2 {
		return nil, nil
	}

	return &MouseButtonEvent{
		Button: MouseButton(data[0]),
		Action: MouseButtonAction(data[1]),
	}, nil
}

// ParseMouseScrollData parses binary mouse scroll data
func ParseMouseScrollData(data []byte) (*MouseScrollEvent, error) {
	if len(data) < 2 {
		return nil, nil
	}

	return &MouseScrollEvent{
		Amount: int16(binary.LittleEndian.Uint16(data[0:2])),
	}, nil
}
