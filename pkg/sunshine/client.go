package sunshine

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client communicates with a Sunshine server
type Client struct {
	host      string
	httpPort  int
	httpsPort int

	httpClient  *http.Client
	httpsClient *http.Client

	// Client identity
	uniqueID string
	uuid     string

	// Paired certificate (used for HTTPS after pairing)
	clientCert tls.Certificate
	serverCert *x509.Certificate
}

// NewClient creates a new Sunshine client
func NewClient(host string, httpPort, httpsPort int) *Client {
	return &Client{
		host:      host,
		httpPort:  httpPort,
		httpsPort: httpsPort,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		httpsClient: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true, // Sunshine uses self-signed certs
				},
			},
		},
		uniqueID: "0123456789ABCDEF",
		uuid:     generateUUID(),
	}
}

// SetClientCertificate sets the client certificate for authenticated requests
func (c *Client) SetClientCertificate(cert tls.Certificate) {
	c.clientCert = cert
	c.httpsClient = &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
				Certificates:       []tls.Certificate{cert},
			},
		},
	}
}

// ServerInfo contains information about the Sunshine server
type ServerInfo struct {
	Hostname            string
	AppVersion          string
	GfeVersion          string
	UniqueID            string
	HttpsPort           int
	ExternalPort        int
	MAC                 string
	LocalIP             string
	ServerCodecSupport  int
	PairStatus          bool
	CurrentGame         int
	State               string
	MaxLumaPixelsHEVC   int
}

// App represents an application on the Sunshine server
type App struct {
	ID           int
	Title        string
	IsHDRSupport bool
}

// xmlRoot is the root element of Sunshine API responses
type xmlRoot struct {
	XMLName       xml.Name `xml:"root"`
	StatusCode    int      `xml:"status_code,attr"`
	StatusMessage string   `xml:"status_message,attr,omitempty"`

	// ServerInfo fields
	Hostname           string `xml:"hostname,omitempty"`
	AppVersion         string `xml:"appversion,omitempty"`
	GfeVersion         string `xml:"GfeVersion,omitempty"`
	UniqueID           string `xml:"uniqueid,omitempty"`
	HttpsPort          string `xml:"HttpsPort,omitempty"`
	ExternalPort       string `xml:"ExternalPort,omitempty"`
	MAC                string `xml:"mac,omitempty"`
	LocalIP            string `xml:"LocalIP,omitempty"`
	ServerCodecSupport string `xml:"ServerCodecModeSupport,omitempty"`
	PairStatus         string `xml:"PairStatus,omitempty"`
	CurrentGame        string `xml:"currentgame,omitempty"`
	State              string `xml:"state,omitempty"`
	MaxLumaPixelsHEVC  string `xml:"MaxLumaPixelsHEVC,omitempty"`

	// Pairing fields
	Paired            string `xml:"paired,omitempty"`
	PlainCert         string `xml:"plaincert,omitempty"`
	ChallengeResponse string `xml:"challengeresponse,omitempty"`
	PairingSecret     string `xml:"pairingsecret,omitempty"`

	// Launch fields
	GameSession string `xml:"gamesession,omitempty"`
	SessionURL0 string `xml:"sessionUrl0,omitempty"`
	Resume      string `xml:"resume,omitempty"`
	Cancel      string `xml:"cancel,omitempty"`

	// App list
	Apps []xmlApp `xml:"App,omitempty"`
}

type xmlApp struct {
	ID             string `xml:"ID"`
	Title          string `xml:"AppTitle"`
	IsHDRSupported string `xml:"IsHdrSupported,omitempty"`
}

func (c *Client) httpURL(endpoint string) string {
	return fmt.Sprintf("http://%s:%d/%s", c.host, c.httpPort, endpoint)
}

func (c *Client) httpsURL(endpoint string) string {
	return fmt.Sprintf("https://%s:%d/%s", c.host, c.httpsPort, endpoint)
}

func (c *Client) addClientParams(params url.Values) {
	params.Set("uniqueid", c.uniqueID)
	params.Set("uuid", c.uuid)
}

func (c *Client) doRequest(client *http.Client, baseURL string, params url.Values) (*xmlRoot, error) {
	reqURL := baseURL
	if len(params) > 0 {
		reqURL = baseURL + "?" + params.Encode()
	}

	resp, err := client.Get(reqURL)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var root xmlRoot
	if err := xml.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("parsing XML: %w", err)
	}

	if root.StatusCode/100 == 4 {
		msg := root.StatusMessage
		if msg == "" {
			msg = "request failed"
		}
		return nil, fmt.Errorf("server error %d: %s", root.StatusCode, msg)
	}

	return &root, nil
}

// GetServerInfo queries Sunshine for server information
func (c *Client) GetServerInfo() (*ServerInfo, error) {
	params := url.Values{}
	c.addClientParams(params)

	root, err := c.doRequest(c.httpClient, c.httpURL("serverinfo"), params)
	if err != nil {
		return nil, err
	}

	info := &ServerInfo{
		Hostname:   root.Hostname,
		AppVersion: root.AppVersion,
		GfeVersion: root.GfeVersion,
		UniqueID:   root.UniqueID,
		MAC:        root.MAC,
		LocalIP:    root.LocalIP,
		State:      root.State,
	}

	if v, err := strconv.Atoi(root.HttpsPort); err == nil {
		info.HttpsPort = v
	}
	if v, err := strconv.Atoi(root.ExternalPort); err == nil {
		info.ExternalPort = v
	}
	if v, err := strconv.Atoi(root.ServerCodecSupport); err == nil {
		info.ServerCodecSupport = v
	}
	if v, err := strconv.Atoi(root.PairStatus); err == nil {
		info.PairStatus = v == 1
	}
	if v, err := strconv.Atoi(root.CurrentGame); err == nil {
		info.CurrentGame = v
	}
	if v, err := strconv.Atoi(root.MaxLumaPixelsHEVC); err == nil {
		info.MaxLumaPixelsHEVC = v
	}

	return info, nil
}

// GetAppList retrieves the list of available applications
func (c *Client) GetAppList() ([]App, error) {
	params := url.Values{}
	c.addClientParams(params)

	root, err := c.doRequest(c.httpsClient, c.httpsURL("applist"), params)
	if err != nil {
		return nil, err
	}

	apps := make([]App, 0, len(root.Apps))
	for _, a := range root.Apps {
		app := App{
			Title: a.Title,
		}
		if id, err := strconv.Atoi(a.ID); err == nil {
			app.ID = id
		}
		if a.IsHDRSupported == "1" {
			app.IsHDRSupport = true
		}
		apps = append(apps, app)
	}

	return apps, nil
}

// LaunchRequest contains parameters for launching an application
type LaunchRequest struct {
	AppID      int
	Width      int
	Height     int
	FPS        int
	Bitrate    int
	RIKey      [16]byte
	RIKeyID    uint32
	LocalAudio bool
	Gamepads   int
}

// LaunchResponse contains the result of launching an application
type LaunchResponse struct {
	SessionID      int
	SessionURL     string
}

// Launch starts streaming an application
func (c *Client) Launch(req LaunchRequest) (*LaunchResponse, error) {
	params := url.Values{}
	c.addClientParams(params)

	params.Set("appid", strconv.Itoa(req.AppID))
	params.Set("mode", fmt.Sprintf("%dx%dx%d", req.Width, req.Height, req.FPS))
	params.Set("additionalStates", "1")
	params.Set("sops", "1")
	params.Set("rikey", strings.ToUpper(hex.EncodeToString(req.RIKey[:])))
	params.Set("rikeyid", strconv.FormatUint(uint64(req.RIKeyID), 10))

	if req.LocalAudio {
		params.Set("localAudioPlayMode", "1")
	} else {
		params.Set("localAudioPlayMode", "0")
	}

	params.Set("remoteControllersBitmap", strconv.Itoa(req.Gamepads))
	params.Set("gcmap", strconv.Itoa(req.Gamepads))
	params.Set("gcpersist", "0")

	root, err := c.doRequest(c.httpsClient, c.httpsURL("launch"), params)
	if err != nil {
		return nil, err
	}

	resp := &LaunchResponse{
		SessionURL: root.SessionURL0,
	}
	if v, err := strconv.Atoi(root.GameSession); err == nil {
		resp.SessionID = v
	}

	return resp, nil
}

// Resume resumes an existing streaming session
func (c *Client) Resume(req LaunchRequest) (*LaunchResponse, error) {
	params := url.Values{}
	c.addClientParams(params)

	params.Set("appid", strconv.Itoa(req.AppID))
	params.Set("mode", fmt.Sprintf("%dx%dx%d", req.Width, req.Height, req.FPS))
	params.Set("additionalStates", "1")
	params.Set("sops", "1")
	params.Set("rikey", strings.ToUpper(hex.EncodeToString(req.RIKey[:])))
	params.Set("rikeyid", strconv.FormatUint(uint64(req.RIKeyID), 10))

	if req.LocalAudio {
		params.Set("localAudioPlayMode", "1")
	} else {
		params.Set("localAudioPlayMode", "0")
	}

	params.Set("remoteControllersBitmap", strconv.Itoa(req.Gamepads))
	params.Set("gcmap", strconv.Itoa(req.Gamepads))
	params.Set("gcpersist", "0")

	root, err := c.doRequest(c.httpsClient, c.httpsURL("resume"), params)
	if err != nil {
		return nil, err
	}

	resp := &LaunchResponse{
		SessionURL: root.SessionURL0,
	}
	if v, err := strconv.Atoi(root.Resume); err == nil {
		resp.SessionID = v
	}

	return resp, nil
}

// Cancel stops the current streaming session
func (c *Client) Cancel() error {
	params := url.Values{}
	c.addClientParams(params)

	_, err := c.doRequest(c.httpsClient, c.httpsURL("cancel"), params)
	return err
}

// Unpair removes the pairing with the server
func (c *Client) Unpair() error {
	params := url.Values{}
	c.addClientParams(params)

	_, err := c.doRequest(c.httpClient, c.httpURL("unpair"), params)
	return err
}

func generateUUID() string {
	// Simple UUID v4 generation
	b := make([]byte, 16)
	for i := range b {
		b[i] = byte(time.Now().UnixNano() >> (i * 8))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
