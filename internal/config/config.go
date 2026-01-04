package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds the application configuration
type Config struct {
	Sunshine SunshineConfig `yaml:"sunshine"`
	WebRTC   WebRTCConfig   `yaml:"webrtc"`
	Server   ServerConfig   `yaml:"server"`
	Stream   StreamConfig   `yaml:"stream"`
}

// SunshineConfig holds Sunshine server connection settings
type SunshineConfig struct {
	Host       string `yaml:"host"`
	HTTPPort   int    `yaml:"http_port"`
	HTTPSPort  int    `yaml:"https_port"`
	ClientCert string `yaml:"client_cert"`
	ClientKey  string `yaml:"client_key"`
}

// ICEServer represents a STUN/TURN server configuration
type ICEServer struct {
	URLs       []string `yaml:"urls"`
	Username   string   `yaml:"username,omitempty"`
	Credential string   `yaml:"credential,omitempty"`
}

// PortRange defines a range of ports
type PortRange struct {
	Min uint16 `yaml:"min"`
	Max uint16 `yaml:"max"`
}

// WebRTCConfig holds WebRTC settings
type WebRTCConfig struct {
	ICEServers []ICEServer `yaml:"ice_servers"`
	PortRange  *PortRange  `yaml:"port_range,omitempty"`
}

// ServerConfig holds HTTP server settings
type ServerConfig struct {
	BindAddress string `yaml:"bind_address"`
	TLSCert     string `yaml:"tls_cert,omitempty"`
	TLSKey      string `yaml:"tls_key,omitempty"`
}

// StreamConfig holds default streaming settings
type StreamConfig struct {
	DefaultApp     string `yaml:"default_app"`
	DefaultBitrate int    `yaml:"default_bitrate"`
	DefaultFPS     int    `yaml:"default_fps"`
	DefaultWidth   int    `yaml:"default_width"`
	DefaultHeight  int    `yaml:"default_height"`
}

// DefaultConfig returns a configuration with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		Sunshine: SunshineConfig{
			Host:      "localhost",
			HTTPPort:  47989,
			HTTPSPort: 47984,
		},
		WebRTC: WebRTCConfig{
			ICEServers: []ICEServer{
				{URLs: []string{
					"stun:stun.l.google.com:19302",
					"stun:stun1.l.google.com:19302",
				}},
			},
		},
		Server: ServerConfig{
			BindAddress: "0.0.0.0:8080",
		},
		Stream: StreamConfig{
			DefaultApp:     "Desktop",
			DefaultBitrate: 10000,
			DefaultFPS:     60,
			DefaultWidth:   1920,
			DefaultHeight:  1080,
		},
	}
}

// Load reads configuration from a YAML file
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Save writes configuration to a YAML file
func (c *Config) Save(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
