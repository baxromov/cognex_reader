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
	upgrader         = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	connectedClients = make(map[*websocket.Conn]bool)
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

	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	})

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
	retryInterval := 5 * time.Second

	var conn net.Conn
	var err error

	// Infinite retry loop until successful connection
	for {
		log.Println("Attempting to connect to Telnet server...")
		notifyClients("Attempting to connect to Telnet server...")

		conn, err = net.Dial("tcp", host)
		if err != nil {
			log.Printf("Failed to connect to Telnet server: %v. Retrying in %v...", err, retryInterval)
			notifyClients("Failed to connect to Telnet server: " + err.Error() + ". Retrying...")
			time.Sleep(retryInterval)
			continue
		}
		break // Break the loop once connected successfully
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)

	log.Println("Sending login credentials...")
	notifyClients("Sending login credentials...")

	// Send login credentials
	if _, err := conn.Write([]byte(username + "\n")); err != nil {
		errorMessage := "Failed to send username: " + err.Error()
		log.Println(errorMessage)
		notifyClients(errorMessage)
		return
	}
	if _, err := conn.Write([]byte(password + "\n")); err != nil {
		errorMessage := "Failed to send password: " + err.Error()
		log.Println(errorMessage)
		notifyClients(errorMessage)
		return
	}

	// Check login response
	for {
		commandToSend := "+\n"
		if _, err := conn.Write([]byte(commandToSend)); err != nil {
			log.Printf("Error sending command to Telnet: %v", err)
			notifyClients("Error sending command to Telnet: " + err.Error())
			return
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			errorMessage := "Error reading from Telnet: " + err.Error()
			log.Println(errorMessage)
			notifyClients(errorMessage)
			return
		}
		log.Printf("Received from Telnet: %s", line)
		if line == "Login succeeded\n" {
			break
		} else {
			log.Println("Login failed or incomplete, received:", line)
		}
		notifyClients(line)
	}

	// Main loop to interact with Telnet server
	for {
		commandToSend := "+\n"
		if _, err := conn.Write([]byte(commandToSend)); err != nil {
			log.Printf("Error sending command to Telnet: %v", err)
			notifyClients("Error sending command to Telnet: " + err.Error())
			return
		}

		response, err := reader.ReadString('\n')
		if err != nil {
			log.Fatalf("Error reading from Telnet: %v", err)
		}
		log.Printf("Received from Telnet: %s", response)

		notifyClients(response)
	}
}
func main() {
	http.HandleFunc("/ws", websocketHandler)
	go handleTelnet()

	log.Println("WebSocket server running on ws://0.0.0.0:8765")
	if err := http.ListenAndServe(":8765", nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
