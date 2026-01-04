package rtsp

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Client handles RTSP communication with Sunshine
type Client struct {
	mu sync.Mutex

	conn    net.Conn
	reader  *bufio.Reader
	writer  *bufio.Writer

	sessionID string
	cseq      int
	serverURL string

	// Callbacks
	onVideoRTP func(data []byte)
	onAudioRTP func(data []byte)

	// RTP receivers
	videoConn net.PacketConn
	audioConn net.PacketConn

	running   bool
	closeChan chan struct{}
}

// SDPMedia represents a media description from SDP
type SDPMedia struct {
	Type       string // "video" or "audio"
	Port       int
	Protocol   string
	Format     string
	Control    string
	Codec      string
	ClockRate  int
	Channels   int
}

// NewClient creates a new RTSP client
func NewClient(serverURL string) *Client {
	return &Client{
		serverURL: serverURL,
		cseq:      1,
		closeChan: make(chan struct{}),
	}
}

// Connect establishes connection to the RTSP server
func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Parse URL to get host:port
	url := c.serverURL
	url = strings.TrimPrefix(url, "rtsp://")

	// Default port
	host := url
	if !strings.Contains(host, ":") {
		host = host + ":48010"
	}

	conn, err := net.DialTimeout("tcp", host, 10*time.Second)
	if err != nil {
		return fmt.Errorf("connecting to RTSP server: %w", err)
	}

	c.conn = conn
	c.reader = bufio.NewReader(conn)
	c.writer = bufio.NewWriter(conn)

	return nil
}

// OnVideoRTP sets the callback for video RTP packets
func (c *Client) OnVideoRTP(fn func(data []byte)) {
	c.onVideoRTP = fn
}

// OnAudioRTP sets the callback for audio RTP packets
func (c *Client) OnAudioRTP(fn func(data []byte)) {
	c.onAudioRTP = fn
}

// Describe sends DESCRIBE request and returns SDP
func (c *Client) Describe() ([]SDPMedia, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	req := c.buildRequest("DESCRIBE", c.serverURL)
	req += "Accept: application/sdp\r\n"
	req += "\r\n"

	resp, body, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("DESCRIBE failed: %d %s", resp.StatusCode, resp.StatusText)
	}

	return parseSDP(body), nil
}

// Setup sets up a media stream
func (c *Client) Setup(media *SDPMedia, clientPort int) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	controlURL := c.serverURL
	if media.Control != "" && !strings.HasPrefix(media.Control, "rtsp://") {
		controlURL = c.serverURL + "/" + media.Control
	}

	req := c.buildRequest("SETUP", controlURL)
	req += fmt.Sprintf("Transport: RTP/AVP;unicast;client_port=%d-%d\r\n", clientPort, clientPort+1)
	if c.sessionID != "" {
		req += fmt.Sprintf("Session: %s\r\n", c.sessionID)
	}
	req += "\r\n"

	resp, _, err := c.sendRequest(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("SETUP failed: %d %s", resp.StatusCode, resp.StatusText)
	}

	// Extract session ID
	if session, ok := resp.Headers["Session"]; ok {
		// Remove timeout parameter if present
		if idx := strings.Index(session, ";"); idx != -1 {
			session = session[:idx]
		}
		c.sessionID = session
	}

	return nil
}

// Play starts playback
func (c *Client) Play() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	req := c.buildRequest("PLAY", c.serverURL)
	req += fmt.Sprintf("Session: %s\r\n", c.sessionID)
	req += "Range: npt=0.000-\r\n"
	req += "\r\n"

	resp, _, err := c.sendRequest(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("PLAY failed: %d %s", resp.StatusCode, resp.StatusText)
	}

	return nil
}

// Teardown stops the session
func (c *Client) Teardown() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sessionID == "" {
		return nil
	}

	req := c.buildRequest("TEARDOWN", c.serverURL)
	req += fmt.Sprintf("Session: %s\r\n", c.sessionID)
	req += "\r\n"

	resp, _, err := c.sendRequest(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("TEARDOWN failed: %d %s", resp.StatusCode, resp.StatusText)
	}

	c.sessionID = ""
	return nil
}

// StartRTPReceiver starts receiving RTP packets on the specified port
func (c *Client) StartRTPReceiver(mediaType string, port int) error {
	conn, err := net.ListenPacket("udp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("binding to port %d: %w", port, err)
	}

	if mediaType == "video" {
		c.videoConn = conn
	} else {
		c.audioConn = conn
	}

	c.running = true
	go c.receiveRTP(conn, mediaType)

	return nil
}

func (c *Client) receiveRTP(conn net.PacketConn, mediaType string) {
	buf := make([]byte, 65536)

	for {
		select {
		case <-c.closeChan:
			return
		default:
		}

		conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			if c.running {
				continue
			}
			return
		}

		// Make a copy of the data
		data := make([]byte, n)
		copy(data, buf[:n])

		if mediaType == "video" && c.onVideoRTP != nil {
			c.onVideoRTP(data)
		} else if mediaType == "audio" && c.onAudioRTP != nil {
			c.onAudioRTP(data)
		}
	}
}

// Close closes the RTSP client
func (c *Client) Close() error {
	c.running = false
	close(c.closeChan)

	if c.videoConn != nil {
		c.videoConn.Close()
	}
	if c.audioConn != nil {
		c.audioConn.Close()
	}

	c.Teardown()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// Response represents an RTSP response
type Response struct {
	StatusCode int
	StatusText string
	Headers    map[string]string
}

func (c *Client) buildRequest(method, url string) string {
	req := fmt.Sprintf("%s %s RTSP/1.0\r\n", method, url)
	req += fmt.Sprintf("CSeq: %d\r\n", c.cseq)
	req += fmt.Sprintf("User-Agent: Gamelight/1.0\r\n")
	c.cseq++
	return req
}

func (c *Client) sendRequest(req string) (*Response, string, error) {
	_, err := c.writer.WriteString(req)
	if err != nil {
		return nil, "", err
	}
	if err := c.writer.Flush(); err != nil {
		return nil, "", err
	}

	return c.readResponse()
}

func (c *Client) readResponse() (*Response, string, error) {
	// Read status line
	line, err := c.reader.ReadString('\n')
	if err != nil {
		return nil, "", err
	}
	line = strings.TrimSpace(line)

	parts := strings.SplitN(line, " ", 3)
	if len(parts) < 3 {
		return nil, "", fmt.Errorf("invalid status line: %s", line)
	}

	statusCode, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, "", fmt.Errorf("invalid status code: %s", parts[1])
	}

	resp := &Response{
		StatusCode: statusCode,
		StatusText: parts[2],
		Headers:    make(map[string]string),
	}

	// Read headers
	contentLength := 0
	for {
		line, err := c.reader.ReadString('\n')
		if err != nil {
			return nil, "", err
		}
		line = strings.TrimSpace(line)

		if line == "" {
			break
		}

		colonIdx := strings.Index(line, ":")
		if colonIdx > 0 {
			key := strings.TrimSpace(line[:colonIdx])
			value := strings.TrimSpace(line[colonIdx+1:])
			resp.Headers[key] = value

			if strings.EqualFold(key, "Content-Length") {
				contentLength, _ = strconv.Atoi(value)
			}
		}
	}

	// Read body if present
	var body string
	if contentLength > 0 {
		bodyBytes := make([]byte, contentLength)
		_, err := io.ReadFull(c.reader, bodyBytes)
		if err != nil {
			return nil, "", err
		}
		body = string(bodyBytes)
	}

	return resp, body, nil
}

func parseSDP(sdp string) []SDPMedia {
	var media []SDPMedia
	var current *SDPMedia

	lines := strings.Split(sdp, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) < 2 || line[1] != '=' {
			continue
		}

		key := line[0]
		value := line[2:]

		switch key {
		case 'm':
			// Media description: m=<media> <port> <proto> <fmt>
			parts := strings.Fields(value)
			if len(parts) >= 4 {
				port, _ := strconv.Atoi(parts[1])
				m := SDPMedia{
					Type:     parts[0],
					Port:     port,
					Protocol: parts[2],
					Format:   parts[3],
				}
				media = append(media, m)
				current = &media[len(media)-1]
			}

		case 'a':
			if current == nil {
				continue
			}
			// Attribute
			if strings.HasPrefix(value, "control:") {
				current.Control = strings.TrimPrefix(value, "control:")
			} else if strings.HasPrefix(value, "rtpmap:") {
				// Parse rtpmap: <payload> <encoding>/<clock-rate>[/<channels>]
				parts := strings.SplitN(value, " ", 2)
				if len(parts) == 2 {
					encoding := parts[1]
					codecParts := strings.Split(encoding, "/")
					if len(codecParts) >= 2 {
						current.Codec = codecParts[0]
						current.ClockRate, _ = strconv.Atoi(codecParts[1])
						if len(codecParts) >= 3 {
							current.Channels, _ = strconv.Atoi(codecParts[2])
						}
					}
				}
			}
		}
	}

	return media
}

// GenerateNonce generates a random nonce for authentication
func GenerateNonce() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}
