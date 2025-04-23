package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

func main() {
	listenAddress := "192.168.0.103:2323"

	// Start the mock Telnet server
	listener, err := net.Listen("tcp", listenAddress)
	if err != nil {
		log.Fatalf("Failed to start mock Telnet server: %v", err)
	}
	defer listener.Close()

	log.Printf("Mock Telnet server listening on %s...", listenAddress)

	for {
		// Accept client connection
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Failed to accept connection: %v", err)
			continue
		}

		log.Println("Client connected. Simulating Telnet interaction...")

		// Handle each connection in a separate goroutine
		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	// Synchronization primitive to stop the data stream when the client disconnects
	stopSignal := make(chan bool)
	var wg sync.WaitGroup

	// Simulate login prompt
	writer.WriteString("Username: ")
	writer.Flush()

	username, _ := reader.ReadString('\n')
	username = strings.TrimSpace(username)
	log.Printf("Received username: %s", username)

	writer.WriteString("Password: ")
	writer.Flush()

	password, _ := reader.ReadString('\n')
	password = strings.TrimSpace(password)
	log.Printf("Received password: %s", password)

	// Authenticate the user
	if username == "admin" && password == "" {
		writer.WriteString("Login successful!\r\n")
	} else {
		writer.WriteString("Login failed!\r\n")
		writer.Flush()
		conn.Close()
		return
	}
	writer.Flush()

	log.Println("User authenticated successfully.")

	// Simulate sending periodic data
	wg.Add(1)
	go func() {
		defer wg.Done() // Ensure goroutine completes when stopped
		for {
			select {
			case <-stopSignal: // Stop data stream when signal is received
				log.Println("Stopping simulated data stream...")
				return
			default:
				time.Sleep(2 * time.Second) // Send data every 2 seconds
				writer.WriteString("ZE-05-W-CM-RFID\r\n")
				writer.Flush()
			}
		}
	}()

	// Continuously handle client commands
	for {
		// Read client command
		command, err := reader.ReadString('\n')
		if err != nil {
			log.Printf("Client disconnected: %v", err)
			break // Exit command loop if client disconnects
		}

		trimmedCommand := strings.TrimSpace(command)
		log.Printf("Received command from client: %s", trimmedCommand)

		// Respond to specific commands
		switch strings.ToLower(trimmedCommand) {
		case "ping":
			writer.WriteString("PONG\r\n")
		case "status":
			writer.WriteString("Server Status: OK\r\n")
		case "exit", "quit":
			writer.WriteString("Goodbye!\r\n")
			writer.Flush()
			break
		default:
			writer.WriteString(fmt.Sprintf("Unknown command: %s\r\n", trimmedCommand))
		}

		writer.Flush()
	}

	close(stopSignal)
	wg.Wait()
	log.Println("Connection closed. Cleaned up all resources.")
}
