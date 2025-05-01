package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var (
	upgrader      = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	channels      = make(map[string]*TelnetChannel) // Channel name to TelnetChannel mapping
	mu            sync.Mutex
	retryInterval = 5 * time.Second
)

// TelnetChannel represents a single channel managing Telnet connections and WebSocket clients
type TelnetChannel struct {
	host         string                   // Host to connect to for Telnet
	clients      map[*websocket.Conn]bool // Active WebSocket clients connected to this channel
	commandChan  chan string              // Commands sent via WebSocket to Telnet
	stopSignal   chan bool                // Signal to stop the Telnet handler
	telnetActive bool                     // Track if Telnet is running for this channel
	mu           sync.Mutex               // Mutex for safe access to channel data
}

// broadcastMessage sends a message to all WebSocket clients in a channel
func (tc *TelnetChannel) broadcastMessage(message string) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	for client := range tc.clients {
		if err := client.WriteMessage(websocket.TextMessage, []byte(message)); err != nil {
			log.Printf("Error sending message to client: %v", err)
			client.Close()
			delete(tc.clients, client) // Remove disconnected client
		}
	}
}

// addClient adds a WebSocket client to the channel
func (tc *TelnetChannel) addClient(conn *websocket.Conn) {
	tc.mu.Lock()
	tc.clients[conn] = true
	tc.mu.Unlock()
}

// removeClient removes a WebSocket client from the channel
func (tc *TelnetChannel) removeClient(conn *websocket.Conn) {
	tc.mu.Lock()
	delete(tc.clients, conn)
	noClients := len(tc.clients) == 0
	tc.mu.Unlock()

	// If no clients remain, close the Telnet connection and clean up the channel
	if noClients {
		log.Printf("No more clients in channel. Shutting down Telnet for channel with host '%s'.", tc.host)
		mu.Lock()
		delete(channels, tc.host) // Remove the channel from the global channel map
		mu.Unlock()

		// Signal Telnet handler to stop and clean up resources
		close(tc.stopSignal)
	}
}

func websocketHandler(w http.ResponseWriter, r *http.Request) {
	// Extract channel name from the URL path
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) == 0 || pathParts[0] == "" {
		http.Error(w, "Missing channel in path", http.StatusBadRequest)
		return
	}
	channelName := pathParts[0]

	// Check for host in query parameters
	host := r.URL.Query().Get("host")

	// Upgrade the HTTP connection to a WebSocket connection
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Error upgrading to WebSocket: %v", err)
		return
	}
	defer conn.Close()

	// If no host is provided, notify the client and keep the connection open
	if host == "" {
		log.Printf("Client connected to channel '%s' without specifying a Telnet host.", channelName)
		conn.WriteMessage(websocket.TextMessage, []byte("No Telnet host specified. Provide a 'host' query parameter to connect to a Telnet server."))
		return
	}

	// Ensure channel exists; create one if it doesn't
	mu.Lock()
	channel, exists := channels[channelName]
	if !exists {
		channel = &TelnetChannel{
			host:        host,
			clients:     make(map[*websocket.Conn]bool),
			commandChan: make(chan string),
			stopSignal:  make(chan bool),
		}
		channels[channelName] = channel
		go handleTelnetConnection(channel, channelName) // Start Telnet handler
	}
	mu.Unlock()

	// Add client to the channel
	channel.addClient(conn)

	// Listen for messages from the WebSocket client
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			log.Printf("Error reading message from client: %v. Disconnecting client.", err)
			break
		}
		// Relay WebSocket command to the Telnet handler
		channel.commandChan <- string(message)
	}

	// Cleanup when client disconnects
	channel.removeClient(conn)
}

func handleTelnetConnection(tc *TelnetChannel, channelName string) {
	tc.mu.Lock()
	if tc.telnetActive {
		tc.mu.Unlock()
		return // Telnet handler already running for this channel
	}
	tc.telnetActive = true
	tc.mu.Unlock()

	defer func() {
		tc.mu.Lock()
		tc.telnetActive = false
		tc.mu.Unlock()
	}()

	for {
		log.Printf("Connecting to Telnet server (%s) for channel '%s'...", tc.host, channelName)

		conn, err := net.Dial("tcp", tc.host)
		if err != nil {
			// Check if the stop signal has been triggered
			select {
			case <-tc.stopSignal:
				log.Printf("Stopping Telnet handler for channel '%s'.", channelName)
				return
			default:
				log.Printf("Failed to connect to Telnet server for channel '%s': %v. Retrying in %v...", channelName, err, retryInterval)
				time.Sleep(retryInterval)
				continue
			}
		}

		reader := bufio.NewReader(conn)
		done := make(chan bool)

		// Goroutine to read and broadcast Telnet responses
		go func() {
			defer close(done)
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					log.Printf("Error reading from Telnet server for channel '%s': %v", channelName, err)
					break
				}
				log.Printf("Telnet (channel '%s'): %s", channelName, line)
				tc.broadcastMessage(line) // Broadcast message to WebSocket clients
			}
		}()

		// Goroutine to monitor and forward WebSocket commands to Telnet
		go func() {
			for {
				select {
				case <-done: // Stop sending if Telnet connection closes
					return
				case <-tc.stopSignal: // Stop the Telnet handler
					log.Printf("Stopping Telnet handler for channel '%s'.", channelName)
					conn.Close()
					return
				case command := <-tc.commandChan: // Forward command to Telnet
					_, err := fmt.Fprint(conn, command+"\n")
					if err != nil {
						log.Printf("Error sending command to Telnet (channel '%s'): %v", channelName, err)
						return
					}
				}
			}
		}()

		// Wait for either Telnet disconnection or stop signal
		select {
		case <-done:
			conn.Close()
			log.Printf("Telnet connection for channel '%s' lost. Retrying...", channelName)
			time.Sleep(retryInterval)
		case <-tc.stopSignal:
			log.Printf("Telnet handler for channel '%s' stopped by signal.", channelName)
			conn.Close()
			return
		}
	}
}

func main() {
	http.HandleFunc("/", websocketHandler) // All paths routed to the handler
	log.Println("WebSocket server running on ws://0.0.0.0:8765/{channelname}?host=<telnet_ip:port>")
	if err := http.ListenAndServe(":8765", nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
