package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var (
	upgrader         = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	connectedClients = make(map[*websocket.Conn]bool)
	mu               sync.Mutex
	commandChannel   = make(chan string) // Channel to send commands to Telnet server
	retryInterval    = 5 * time.Second
)

func notifyClients(message string) {
	mu.Lock()
	defer mu.Unlock()
	for client := range connectedClients {
		if err := client.WriteMessage(websocket.TextMessage, []byte(message)); err != nil {
			log.Printf("Error sending message to client: %v", err)
			client.Close()
			delete(connectedClients, client)
		}
	}
}

func websocketHandler(w http.ResponseWriter, r *http.Request) {
	// Extract host from query parameters
	host := r.URL.Query().Get("host")
	if host == "" {
		http.Error(w, "Missing \"host\" query parameter", http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Error upgrading connection: %v", err)
		return
	}
	defer conn.Close()

	mu.Lock()
	connectedClients[conn] = true
	mu.Unlock()

	// Start Telnet connection with the provided host
	go handleTelnet(host)

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			log.Printf("Error reading message from client: %v", err)
			break
		}
		commandChannel <- string(message) // Send command received from WebSocket to Telnet server
	}

	mu.Lock()
	delete(connectedClients, conn)
	mu.Unlock()
}

func handleTelnet(host string) {
	//username := "admin"
	//password := ""

	for {
		log.Printf("Attempting to connect to Telnet server at %s...", host)
		//notifyClients("Attempting to connect to Telnet server at " + host)

		conn, err := net.Dial("tcp", host)
		if err != nil {
			log.Printf("Failed to connect to Telnet server: %v. Retrying in %v...", err, retryInterval)
			//notifyClients("Failed to connect: " + err.Error())
			time.Sleep(retryInterval) // Wait before retrying
			continue
		}

		reader := bufio.NewReader(conn)

		// Send credentials to the Telnet server
		//time.Sleep(1 * time.Second)
		//fmt.Fprint(conn, username+"\n")
		//time.Sleep(1 * time.Second)
		//fmt.Fprint(conn, password+"\n")

		// Create a channel to signal when the connection is closed
		done := make(chan bool)

		// Goroutine to continuously read data from the Telnet server
		go func() {
			defer close(done) // Signal that reading has stopped
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					log.Println("Error reading response from Telnet:", err)
					//notifyClients("Disconnected from Telnet. Attempting reconnect.")
					break
				}
				log.Println("Received:", line)
				notifyClients(line) // Broadcast the response to WebSocket clients
			}
		}()

		// Monitor commands coming from WebSocket clients and send them to the Telnet server
		go func() {
			for {
				select {
				case <-done: // Stop sending commands if the connection is closed
					return
				case command := <-commandChannel: // Read a command from the WebSocket
					_, err := fmt.Fprint(conn, command+"\n")
					if err != nil {
						log.Printf("Error sending command to Telnet server: %v", err)
						return
					}
				}
			}
		}()

		<-done
		conn.Close()
		log.Println("Connection to Telnet server lost. Retrying...")
		//notifyClients("Disconnected. Retrying in 5 seconds...")
		time.Sleep(retryInterval)
	}
}

func main() {
	http.HandleFunc("/ws", websocketHandler)

	log.Println("WebSocket server running on ws://0.0.0.0:8765")
	if err := http.ListenAndServe(":8765", nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
