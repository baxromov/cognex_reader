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

// TelnetHandler manages communication between WebSocket and Telnet
type TelnetHandler struct {
	telnetAddress   string
	websocketConn   *websocket.Conn
	stopSignal      chan bool
	telnetConnected bool // Tracks the Telnet connection status
	wg              sync.WaitGroup
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

// handleTelnetConnections maintains Telnet connection and forwards data
func (handler *TelnetHandler) handleTelnetConnections() {
	for {
		select {
		case <-handler.stopSignal:
			log.Println("Stopping Telnet connection retries.")
			return
		default:
			// Try connecting to Telnet
			telnetConn, err := net.Dial("tcp", handler.telnetAddress)
			if err != nil {
				log.Printf("Telnet connection error: %v. Retrying in %v...", err, retryInterval)

				// Inform WebSocket channel of the Telnet connection issue
				handler.sendToWebSocket("Telnet connection lost. Retrying...\n")

				time.Sleep(retryInterval)
				continue
			}

			// Mark Telnet connection as active
			handler.telnetConnected = true
			log.Println("Connected to Telnet server. Forwarding data...")

			// Forward Telnet data to WebSocket
			handler.forwardTelnetData(telnetConn)

			// Handle connection loss
			telnetConn.Close()
			handler.telnetConnected = false
			log.Println("Telnet connection closed. Retrying...")
		}
	}
}

// forwardTelnetData handles data exchange between WebSocket and Telnet
func (handler *TelnetHandler) forwardTelnetData(conn net.Conn) {
	reader := bufio.NewReader(conn)
	done := make(chan bool)

	// Start Telnet → WebSocket forwarding
	handler.wg.Add(1)
	go func() {
		defer handler.wg.Done()

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				log.Printf("Telnet connection error: %v", err)
				handler.sendToWebSocket("Telnet connection lost: " + err.Error() + "\n")
				done <- true // Signal connection loss
				return
			}

			log.Printf("Telnet → WebSocket: %s", line)

			// Forward Telnet data to WebSocket
			if err := handler.websocketConn.WriteMessage(websocket.TextMessage, []byte(line)); err != nil {
				log.Printf("Error forwarding to WebSocket: %v", err)
				done <- true
				return
			}
		}
	}()

	// Handle WebSocket → Telnet forwarding
	handler.wg.Add(1)
	go func() {
		defer handler.wg.Done()

		for {
			select {
			case <-handler.stopSignal:
				log.Println("Stopping WebSocket → Telnet forwarding.")
				conn.Close()
				return
			case <-done:
				return
			default:
				_, message, err := handler.websocketConn.ReadMessage()
				if err != nil {
					log.Printf("WebSocket read error: %v", err)
					handler.sendToWebSocket("WebSocket error: " + err.Error() + "\n")
					done <- true
					return
				}

				log.Printf("WebSocket → Telnet: %s", message)

				// Write WebSocket command to Telnet
				_, err = conn.Write(append(message, '\n'))
				if err != nil {
					log.Printf("Error sending to Telnet: %v", err)
					handler.sendToWebSocket("Failed to send command to Telnet: " + err.Error() + "\n")
					done <- true
					return
				}
			}
		}
	}()

	// Wait for connection closure or stop signal
	<-done
	close(done)
}

// Start begins Telnet/WebSocket communication
func (handler *TelnetHandler) Start() {
	handler.stopSignal = make(chan bool)

	// Launch Telnet handler goroutine
	handler.wg.Add(1)
	go func() {
		defer handler.wg.Done()
		handler.handleTelnetConnections()
	}()
}

// Stop terminates the WebSocket <-> Telnet communication
func (handler *TelnetHandler) Stop() {
	close(handler.stopSignal)
	handler.wg.Wait()
	if handler.websocketConn != nil {
		_ = handler.websocketConn.Close()
	}
	log.Println("Service stopped successfully.")
}

// sendToWebSocket sends a message to the WebSocket client
func (handler *TelnetHandler) sendToWebSocket(message string) {
	if handler.websocketConn != nil {
		err := handler.websocketConn.WriteMessage(websocket.TextMessage, []byte(message))
		if err != nil {
			log.Printf("Failed to send message to WebSocket: %v", err)
		}
	}
}

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("Usage: %s <telnet-address>", os.Args[0])
	}

	telnetAddress := os.Args[1]
	channelName := strings.ReplaceAll(telnetAddress, ":", "-") // Replace ":" with "-" for WebSocket channel name

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

	// Graceful shutdown
	stop := make(chan os.Signal, 1)
	<-stop
	handler.Stop()
}
