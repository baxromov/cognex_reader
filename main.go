package main

import (
	"bufio"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var (
	upgrader         = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }} // Allow all origins
	connectedClients  = make(map[*websocket.Conn]bool)
	mu               sync.Mutex
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
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Error upgrading connection: %v", err)
		return
	}
	defer conn.Close()

	mu.Lock()
	connectedClients[conn] = true
	mu.Unlock()

	// Set a pong handler to keep the connection alive
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(60 * time.Second)) // Reset deadline on pong
	})

	// Keep the connection open and read messages from the client
	for {
		if _, _, err := conn.NextReader(); err != nil {
			break
		}
	}

	mu.Lock()
	delete(connectedClients, conn)
	mu.Unlock()
}

func handleTelnet() {
	host := "192.168.1.197:23"
	username := "admin"
	password := ""

	// Connect to the Telnet server
	conn, err := net.Dial("tcp", host)
	if err != nil {
		errorMessage := "Failed to connect to Telnet server: " + err.Error()
		log.Println(errorMessage)
		notifyClients(errorMessage)
		return // Exit the function but keep the server running
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)

	// Send login credentials
	log.Println("Sending login credentials...")
	if _, err := conn.Write([]byte(username + "\n")); err != nil {
		errorMessage := "Failed to send username: " + err.Error()
		log.Println(errorMessage)
		notifyClients(errorMessage)
		return // Exit the function but keep the server running
	}
	if _, err := conn.Write([]byte(password + "\n")); err != nil {
		errorMessage := "Failed to send password: " + err.Error()
		log.Println(errorMessage)
		notifyClients(errorMessage)
		return // Exit the function but keep the server running
	}

	// Read until login succeeds
	for {
		commandToSend := "+\n"
		if _, err := conn.Write([]byte(commandToSend)); err != nil {
			log.Printf("Error sending command to Telnet: %v", err)
			notifyClients("Error sending command to Telnet: " + err.Error())
			return // Exit the function but keep the server running
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			errorMessage := "Error reading from Telnet: " + err.Error()
			log.Println(errorMessage)
			notifyClients(errorMessage)
			return // Exit the function but keep the server running
		}
		log.Printf("Received from Telnet: %s", line)
		if line == "Login succeeded\n" {
			break
		} else {
			log.Println("Login failed or incomplete, received:", line)
		}
	
		// Notify WebSocket clients about the login status
		notifyClients(line)
	}

	// Send commands and handle responses
	for {
		commandToSend := "+\n"
		if _, err := conn.Write([]byte(commandToSend)); err != nil {
			log.Printf("Error sending command to Telnet: %v", err)
			notifyClients("Error sending command to Telnet: " + err.Error())
			return // Exit the function but keep the server running
		}

		response, err := reader.ReadString('\n')
		if err != nil {
			log.Fatalf("Error reading from Telnet: %v", err)
		}
		log.Printf("Received from Telnet: %s", response)

		// Notify WebSocket clients
		notifyClients(response)
	}
}

func main() {
	http.HandleFunc("/ws", websocketHandler)
	go handleTelnet()

	log.Println("WebSocket server running on ws://localhost:8765")
	if err := http.ListenAndServe(":8765", nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}