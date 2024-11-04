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
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Error upgrading connection: %v", err)
		return
	}
	defer conn.Close()

	mu.Lock()
	connectedClients[conn] = true
	mu.Unlock()

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

func handleTelnet() {
	host := "192.168.1.197:23"
	username := "admin"
	password := ""

	for {
		log.Println("Attempting to connect to Telnet server...")
		notifyClients("Attempting to connect to Telnet server...")

		conn, err := net.Dial("tcp", host)
		if err != nil {
			log.Printf("Failed to connect to Telnet server: %v. Retrying in %v...", err, retryInterval)
			notifyClients("Failed to connect: " + err.Error())
			time.Sleep(retryInterval)
			continue
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)

		// Send credentials with delay to avoid missed prompts
		time.Sleep(1 * time.Second)
		fmt.Fprint(conn, username+"\n")
		time.Sleep(1 * time.Second)
		fmt.Fprint(conn, password+"\n")

		// Goroutine to continuously read responses
		go func() {
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					log.Println("Error reading response:", err)
					notifyClients("Disconnected from Telnet. Attempting reconnect.")
					break
				}
				log.Println("Received:", line)
				notifyClients(line)
			}
		}()

		// Loop to send commands
		for {
			select {
			case cmd := <-commandChannel:
				log.Printf("Sending command: %s", cmd)
				if _, err := conn.Write([]byte(cmd + "\n")); err != nil {
					log.Printf("Error sending command: %v", err)
					notifyClients("Failed to send command.")
					break
				}
			case <-time.After(2 * time.Second): // Send "+" if idle
				log.Println("Sending keep-alive command: +")
				if _, err := conn.Write([]byte("+\n")); err != nil {
					log.Printf("Error sending keep-alive: %v", err)
					notifyClients("Disconnected, will retry.")
					break
				}
			}
		}
		// Close connection and retry if the goroutine exits
		conn.Close()
		time.Sleep(retryInterval)
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
