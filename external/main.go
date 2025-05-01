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

var (
	retryInterval = 5 * time.Second
)

// TelnetHandler manages communication between WebSocket and Telnet
type TelnetHandler struct {
	telnetAddress   string
	websocketConn   *websocket.Conn
	stopSignal      chan bool
	telnetConnected bool // Track if Telnet is actively connected
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

// handleTelnetConnections maintains Telnet connection and data forwarding
func (handler *TelnetHandler) handleTelnetConnections() {
	for {
		select {
		case <-handler.stopSignal:
			log.Println("Stopping Telnet connection retries...")
			return
		default:
			// Try connecting to Telnet
			telnetConn, err := net.Dial("tcp", handler.telnetAddress)
			if err != nil {
				log.Printf("Error connecting to Telnet server: %v. Retrying in %v...", err, retryInterval)
				time.Sleep(retryInterval)
				continue
			}

			// If connected successfully, start communication
			log.Println("Connected to Telnet server. Forwarding data...")
			handler.telnetConnected = true
			handler.forwardTelnetData(telnetConn)

			// If Telnet communication ends, reset status
			telnetConn.Close()
			handler.telnetConnected = false
			log.Println("Telnet connection lost. Retrying...")
		}
	}
}

// forwardTelnetData sends & receives data between WebSocket and Telnet
func (handler *TelnetHandler) forwardTelnetData(conn net.Conn) {
	reader := bufio.NewReader(conn)
	done := make(chan bool)

	// Start Telnet -> WebSocket forwarding
	handler.wg.Add(1)
	go func() {
		defer handler.wg.Done()

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				log.Printf("Telnet connection closed: %v", err)
				done <- true
				return
			}

			log.Printf("Telnet -> WebSocket: %s", line)

			// Forward Telnet response to WebSocket
			err = handler.websocketConn.WriteMessage(websocket.TextMessage, []byte(line))
			if err != nil {
				log.Printf("Error forwarding to WebSocket: %v", err)
				done <- true
				return
			}
		}
	}()

	// Handle WebSocket -> Telnet commands
	handler.wg.Add(1)
	go func() {
		defer handler.wg.Done()

		for {
			select {
			case <-handler.stopSignal:
				log.Println("Stopping WebSocket -> Telnet forwarding...")
				conn.Close()
				return
			case <-done:
				return
			default:
				_, message, err := handler.websocketConn.ReadMessage()
				if err != nil {
					log.Printf("Error reading from WebSocket: %v. Closing Telnet connection.", err)
					done <- true
					return
				}

				// Forward WebSocket commands to Telnet server
				log.Printf("WebSocket -> Telnet: %s", string(message))
				_, err = conn.Write(append(message, '\n'))
				if err != nil {
					log.Printf("Error writing to Telnet: %v. Closing connection.", err)
					done <- true
					return
				}
			}
		}
	}()

	// Wait until connection is closed or stop signal received
	<-done
	close(done)
}

// Start begins the WebSocket -> Telnet communication workflow
func (handler *TelnetHandler) Start() {
	handler.stopSignal = make(chan bool)

	// Launch Telnet connection handling in the background
	handler.wg.Add(1)
	go func() {
		defer handler.wg.Done()
		handler.handleTelnetConnections()
	}()
}

// Stop terminates communication and cleans up resources
func (handler *TelnetHandler) Stop() {
	close(handler.stopSignal)
	handler.wg.Wait()
	_ = handler.websocketConn.Close()
	log.Println("Service stopped successfully.")
}

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("Usage: %s <telnet-address>", os.Args[0])
	}

	telnetAddress := os.Args[1]
	channelName := strings.ReplaceAll(telnetAddress, ":", "-") // Replace ":" with "-" for channel name

	// Connect to WebSocket channel
	websocketConn, err := ConnectWebSocket(channelName)
	if err != nil {
		log.Fatalf("Failed to connect to WebSocket server: %v", err)
	}
	log.Printf("Successfully connected to WebSocket channel: %s", channelName)

	// Initialize and start service
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
