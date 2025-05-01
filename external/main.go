package main

import (
	"bufio"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const websocketServerURL = "wss://laundirs-supply-chain-websocket.azurewebsites.net"

var retryInterval = 5 * time.Second

type TelnetHandler struct {
	telnetAddress   string
	websocketConn   *websocket.Conn
	stopSignal      chan struct{} // Graceful shutdown signal
	telnetConnected bool          // Tracks active Telnet connection
	wg              sync.WaitGroup
	mu              sync.Mutex // Guard concurrent access to shared resources
}

// ConnectWebSocket connects to the WebSocket server
func ConnectWebSocket(channel string) (*websocket.Conn, error) {
	url := websocketServerURL + "/" + channel
	log.Printf("Connecting to WebSocket server: %s", url)

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// sendToWebSocket sends a message to the WebSocket client
func (handler *TelnetHandler) sendToWebSocket(message string) {
	handler.mu.Lock()
	defer handler.mu.Unlock()

	if handler.websocketConn != nil {
		err := handler.websocketConn.WriteMessage(websocket.TextMessage, []byte(message))
		if err != nil {
			log.Printf("Error sending message to WebSocket: %v", err)
		}
	}
}

// Start launches Telnet and WebSocket communication handlers
func (handler *TelnetHandler) Start() {
	handler.stopSignal = make(chan struct{})

	handler.wg.Add(1)
	go func() {
		defer handler.wg.Done()
		handler.handleTelnetConnections()
	}()
}

// handleTelnetConnections manages Telnet connectivity and retries
func (handler *TelnetHandler) handleTelnetConnections() {
	for {
		select {
		case <-handler.stopSignal:
			log.Println("Stopping Telnet connection retries.")
			return
		default:
			conn, err := net.Dial("tcp", handler.telnetAddress)
			if err != nil {
				log.Printf("Telnet connection error: %v. Retrying in %v...", err, retryInterval)
				handler.sendToWebSocket("Telnet connection lost. Retrying...\n")
				time.Sleep(retryInterval)
				continue
			}

			log.Println("Connected to Telnet server. Forwarding data...")
			handler.telnetConnected = true

			handler.forwardTelnetData(conn)

			conn.Close()
			handler.telnetConnected = false
			log.Println("Telnet connection closed. Retrying...")
		}
	}
}

// forwardTelnetData handles full-duplex communication between WebSocket and Telnet
func (handler *TelnetHandler) forwardTelnetData(conn net.Conn) {
	reader := bufio.NewReader(conn)
	done := make(chan struct{})

	// Handle Telnet -> WebSocket communication
	handler.wg.Add(1)
	go func() {
		defer handler.wg.Done()
		defer close(done) // Close done channel when done with this goroutine

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				log.Printf("Telnet read error: %v", err)
				handler.sendToWebSocket("Telnet connection error: " + err.Error() + "\n")
				return
			}

			log.Printf("Telnet -> WebSocket: %s", line)
			handler.sendToWebSocket(line)
		}
	}()

	// Handle WebSocket -> Telnet communication
	handler.wg.Add(1)
	go func() {
		defer handler.wg.Done()

		for {
			select {
			case <-handler.stopSignal:
				log.Println("Stopping WebSocket -> Telnet forwarding.")
				conn.Close()
				return
			case <-done:
				log.Println("Telnet connection lost. Closing WebSocket -> Telnet forwarding.")
				return
			default:
				_, msg, err := handler.websocketConn.ReadMessage()
				if err != nil {
					log.Printf("WebSocket read error: %v", err)
					handler.sendToWebSocket("WebSocket connection error: " + err.Error() + "\n")
					return
				}

				if _, err := conn.Write(append(msg, '\n')); err != nil {
					log.Printf("Error sending to Telnet: %v", err)
					handler.sendToWebSocket("Failed to send command to Telnet: " + err.Error() + "\n")
					return
				}

				log.Printf("WebSocket -> Telnet: %s", msg)
			}
		}
	}()

	<-done // Wait for Telnet connection loss
}

// Stop gracefully shuts down the WebSocket <-> Telnet communication
func (handler *TelnetHandler) Stop() {
	close(handler.stopSignal) // Signal stop to all goroutines
	handler.wg.Wait()         // Wait for all goroutines to finish

	handler.mu.Lock()
	defer handler.mu.Unlock()

	if handler.websocketConn != nil {
		_ = handler.websocketConn.Close()
		log.Println("WebSocket connection closed.")
	}

	log.Println("Service stopped successfully.")
}

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("Usage: %s <telnet-address>", os.Args[0])
	}

	telnetAddress := os.Args[1]
	channelName := strings.ReplaceAll(telnetAddress, ":", "-") // Replace ':' with '-' for WebSocket channel

	// Connect to WebSocket server
	websocketConn, err := ConnectWebSocket(channelName)
	if err != nil {
		log.Fatalf("Failed to connect to WebSocket server: %v", err)
	}
	log.Printf("Connected to WebSocket channel: %s", channelName)

	// Create and start Telnet handler
	handler := &TelnetHandler{
		telnetAddress: telnetAddress,
		websocketConn: websocketConn,
	}

	handler.Start()

	// Graceful shutdown handling
	stop := make(chan os.Signal, 1)
	<-stop
	handler.Stop()
}
