package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/gamelight/gamelight/internal/config"
	"github.com/gamelight/gamelight/pkg/input"
	"github.com/gamelight/gamelight/pkg/rtsp"
	"github.com/gamelight/gamelight/pkg/session"
	"github.com/gamelight/gamelight/pkg/sunshine"
	"github.com/gamelight/gamelight/pkg/web"
	rtcfanout "github.com/gamelight/gamelight/pkg/webrtc"
)

func main() {
	// Parse flags
	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	sunshineHost := flag.String("sunshine-host", "", "Sunshine server host (overrides config)")
	bindAddr := flag.String("bind", "", "Server bind address (overrides config)")
	flag.Parse()

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Printf("Warning: Could not load config file: %v", err)
		cfg = config.DefaultConfig()
	}

	// Apply flag overrides
	if *sunshineHost != "" {
		cfg.Sunshine.Host = *sunshineHost
	}
	if *bindAddr != "" {
		cfg.Server.BindAddress = *bindAddr
	}

	// Create Sunshine client
	sunshineClient := sunshine.NewClient(
		cfg.Sunshine.Host,
		cfg.Sunshine.HTTPPort,
		cfg.Sunshine.HTTPSPort,
	)

	// Check Sunshine connection
	log.Printf("Connecting to Sunshine at %s...", cfg.Sunshine.Host)
	info, err := sunshineClient.GetServerInfo()
	if err != nil {
		log.Printf("Warning: Could not connect to Sunshine: %v", err)
		log.Printf("The server will start but streaming won't work until Sunshine is available.")
	} else {
		log.Printf("Connected to Sunshine: %s (version %s)", info.Hostname, info.AppVersion)
		if !info.PairStatus {
			log.Printf("Warning: Not paired with Sunshine. You may need to pair first.")
		}
	}

	// Create web server
	webServer, err := web.NewServer(cfg)
	if err != nil {
		log.Fatalf("Failed to create web server: %v", err)
	}

	// Set up streaming callbacks
	var rtspClient *rtsp.Client
	var videoTrack *webrtc.TrackLocalStaticRTP
	var audioTrack *webrtc.TrackLocalStaticRTP

	webServer.OnStartStream(func(settings session.StreamSettings) error {
		log.Printf("Starting stream with settings: %+v", settings)

		// Find the default app
		apps, err := sunshineClient.GetAppList()
		if err != nil {
			return fmt.Errorf("getting app list: %w", err)
		}

		appID := 0
		for _, app := range apps {
			if app.Title == cfg.Stream.DefaultApp {
				appID = app.ID
				break
			}
		}

		if appID == 0 && len(apps) > 0 {
			// Use first app if default not found
			appID = apps[0].ID
			log.Printf("Default app '%s' not found, using '%s'", cfg.Stream.DefaultApp, apps[0].Title)
		}

		// Generate encryption key
		var riKey [16]byte
		for i := range riKey {
			riKey[i] = byte(i)
		}

		// Launch the stream
		launchResp, err := sunshineClient.Launch(sunshine.LaunchRequest{
			AppID:      appID,
			Width:      settings.Width,
			Height:     settings.Height,
			FPS:        settings.FPS,
			Bitrate:    settings.Bitrate,
			RIKey:      riKey,
			RIKeyID:    1,
			LocalAudio: false,
			Gamepads:   0xF, // All 4 gamepads
		})
		if err != nil {
			return fmt.Errorf("launching stream: %w", err)
		}

		log.Printf("Stream launched, session URL: %s", launchResp.SessionURL)

		// Create video and audio tracks
		videoTrack, err = rtcfanout.CreateVideoTrack(webrtc.MimeTypeH264)
		if err != nil {
			return fmt.Errorf("creating video track: %w", err)
		}

		audioTrack, err = rtcfanout.CreateAudioTrack()
		if err != nil {
			return fmt.Errorf("creating audio track: %w", err)
		}

		// Set tracks on web server
		webServer.SetVideoTrack(videoTrack)
		webServer.SetAudioTrack(audioTrack)

		// Connect to RTSP
		rtspClient = rtsp.NewClient(launchResp.SessionURL)
		if err := rtspClient.Connect(); err != nil {
			return fmt.Errorf("connecting to RTSP: %w", err)
		}

		// Get media descriptions
		media, err := rtspClient.Describe()
		if err != nil {
			rtspClient.Close()
			return fmt.Errorf("RTSP DESCRIBE: %w", err)
		}

		// Setup and start receivers for each media
		videoPort := 47998
		audioPort := 48000

		for _, m := range media {
			switch m.Type {
			case "video":
				if err := rtspClient.Setup(&m, videoPort); err != nil {
					log.Printf("Warning: Failed to setup video: %v", err)
					continue
				}
				rtspClient.OnVideoRTP(func(data []byte) {
					if videoTrack != nil {
						videoTrack.Write(data)
					}
				})
				rtspClient.StartRTPReceiver("video", videoPort)
				log.Printf("Video stream setup on port %d (codec: %s)", videoPort, m.Codec)

			case "audio":
				if err := rtspClient.Setup(&m, audioPort); err != nil {
					log.Printf("Warning: Failed to setup audio: %v", err)
					continue
				}
				rtspClient.OnAudioRTP(func(data []byte) {
					if audioTrack != nil {
						audioTrack.Write(data)
					}
				})
				rtspClient.StartRTPReceiver("audio", audioPort)
				log.Printf("Audio stream setup on port %d (codec: %s)", audioPort, m.Codec)
			}
		}

		// Start playback
		if err := rtspClient.Play(); err != nil {
			rtspClient.Close()
			return fmt.Errorf("RTSP PLAY: %w", err)
		}

		log.Printf("Stream started successfully")
		return nil
	})

	webServer.OnStopStream(func() {
		log.Printf("Stopping stream...")

		if rtspClient != nil {
			rtspClient.Close()
			rtspClient = nil
		}

		sunshineClient.Cancel()

		videoTrack = nil
		audioTrack = nil

		log.Printf("Stream stopped")
	})

	// Set up input handlers
	inputHandler := webServer.InputHandler()
	setupInputForwarding(inputHandler, sunshineClient)

	// Create HTTP server
	srv := &http.Server{
		Addr:    cfg.Server.BindAddress,
		Handler: webServer.Router(),
	}

	// Start server
	go func() {
		log.Printf("Starting Gamelight server on %s", cfg.Server.BindAddress)
		log.Printf("Open http://%s in your browser", cfg.Server.BindAddress)

		var err error
		if cfg.Server.TLSCert != "" && cfg.Server.TLSKey != "" {
			err = srv.ListenAndServeTLS(cfg.Server.TLSCert, cfg.Server.TLSKey)
		} else {
			err = srv.ListenAndServe()
		}

		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if rtspClient != nil {
		rtspClient.Close()
	}
	sunshineClient.Cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server stopped")
}

func setupInputForwarding(handler *input.Handler, client *sunshine.Client) {
	// TODO: Forward input to Sunshine via the control channel
	// This requires implementing the encrypted control protocol
	// For now, we just log the input events

	handler.OnMouseMove(func(e input.MouseMoveEvent) {
		// Forward to Sunshine
		log.Printf("Mouse move: dx=%d, dy=%d", e.DeltaX, e.DeltaY)
	})

	handler.OnMouseButton(func(e input.MouseButtonEvent) {
		log.Printf("Mouse button: %d, action=%d", e.Button, e.Action)
	})

	handler.OnKeyboard(func(e input.KeyboardEvent) {
		log.Printf("Keyboard: code=%d, action=%d", e.KeyCode, e.Action)
	})

	handler.OnController(func(e input.ControllerEvent) {
		log.Printf("Controller %d: buttons=%x, LT=%d, RT=%d, LS=(%d,%d), RS=(%d,%d)",
			e.ControllerNumber, e.Buttons,
			e.LeftTrigger, e.RightTrigger,
			e.LeftStickX, e.LeftStickY,
			e.RightStickX, e.RightStickY)
	})
}
