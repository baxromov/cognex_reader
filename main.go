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
	channels      = make(map[string]*TelnetChannel)
	mu            sync.Mutex
	retryInterval = 5 * time.Second
)

// TelnetChannel represents a Telnet connection and associated WebSocket clients.
type TelnetChannel struct {
	host         string
	clients      map[*websocket.Conn]bool
	commandChan  chan string
	stopSignal   chan bool
	telnetActive bool
	mu           sync.Mutex
}

func (tc *TelnetChannel) broadcastMessage(message string) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	for client := range tc.clients {
		if err := client.WriteMessage(websocket.TextMessage, []byte(message)); err != nil {
			log.Printf("Error sending message to client: %v", err)
			client.Close()
			delete(tc.clients, client)
		}
	}
}

func (tc *TelnetChannel) addClient(conn *websocket.Conn) {
	tc.mu.Lock()
	tc.clients[conn] = true
	tc.mu.Unlock()
}

func (tc *TelnetChannel) removeClient(conn *websocket.Conn) {
	tc.mu.Lock()
	delete(tc.clients, conn)
	noClients := len(tc.clients) == 0
	tc.mu.Unlock()

	if noClients {
		log.Printf("No more clients in channel. Shutting down Telnet for channel with host '%s'.", tc.host)
		mu.Lock()
		delete(channels, tc.host)
		mu.Unlock()

		close(tc.stopSignal)
	}
}

func websocketHandler(w http.ResponseWriter, r *http.Request) {
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) == 0 || pathParts[0] == "" {
		http.Error(w, "Missing channel in path", http.StatusBadRequest)
		return
	}
	channelName := pathParts[0]

	host := r.URL.Query().Get("host")

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Error upgrading to WebSocket: %v", err)
		return
	}
	defer conn.Close()

	if host == "" {
		log.Printf("Client connected to channel '%s' without specifying a Telnet host.", channelName)
		conn.WriteMessage(websocket.TextMessage, []byte("No Telnet host specified. Provide a 'host' query parameter to connect to a Telnet server."))
		return
	}

	mu.Lock()
	channel, exists := channels[channelName]
	if !exists {
		channel = &TelnetChannel{
			host:        host,
			clients:     make(map[*websocket.Conn]bool),
			commandChan: make(chan string),
			stopSignal:  make(chan bool), // Initialize stopSignal for the first time
		}
		channels[channelName] = channel
		go handleTelnetConnection(channel, channelName)
	} else {
		// If stopSignal is closed, reinitialize it
		select {
		case <-channel.stopSignal:
			channel.stopSignal = make(chan bool)
			go handleTelnetConnection(channel, channelName)
		default:
			// No action needed if stopSignal is still open
		}
	}
	mu.Unlock()

	channel.addClient(conn)

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			log.Printf("Error reading message from client: %v. Disconnecting client.", err)
			break
		}

		channel.commandChan <- string(message)
	}

	channel.removeClient(conn)
}

func handleTelnetConnection(tc *TelnetChannel, channelName string) {
	tc.mu.Lock()
	if tc.telnetActive {
		tc.mu.Unlock()
		return
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
			select {
			case <-tc.stopSignal:
				log.Printf("Stopping Telnet handler for channel '%s' (stop signal received).", channelName)
				return
			default:
				log.Printf("Failed to connect to Telnet server for channel '%s': %v. Retrying in %v...", channelName, err, retryInterval)
				time.Sleep(retryInterval)
				continue
			}
		}

		reader := bufio.NewReader(conn)
		done := make(chan bool)

		go func() {
			defer close(done)
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					log.Printf("Error reading from Telnet server for channel '%s': %v", channelName, err)
					break
				}
				log.Printf("Telnet (channel '%s'): %s", channelName, line)
				tc.broadcastMessage(line)
			}
		}()

		go func() {
			for {
				select {
				case <-done:
					return
				case <-tc.stopSignal:
					log.Printf("Stopping Telnet handler for channel '%s'.", channelName)
					conn.Close()
					return
				case command := <-tc.commandChan:
					_, err := fmt.Fprint(conn, command+"\n")
					if err != nil {
						log.Printf("Error sending command to Telnet (channel '%s'): %v. Closing connection.", channelName, err)
						conn.Close()
						return
					}
				}
			}
		}()

		select {
		case <-done:
			conn.Close()
			log.Printf("Telnet connection for channel '%s' lost. Retrying in %v...", channelName, retryInterval)
			time.Sleep(retryInterval)
		case <-tc.stopSignal:
			log.Printf("Telnet handler for channel '%s' stopped by signal.", channelName)
			conn.Close()
			return
		}
	}
}

func main() {
	http.HandleFunc("/", websocketHandler)
	log.Println("WebSocket server running on ws://0.0.0.0:5001/{channelname}?host=<telnet_ip:port>")
	if err := http.ListenAndServe(":5001", nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
