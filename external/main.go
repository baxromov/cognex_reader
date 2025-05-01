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
	mu            sync.Mutex
)

// TelnetHandler manages communication between WebSocket and Telnet
type TelnetHandler struct {
	telnetAddress string
	websocketConn *websocket.Conn
	stopSignal    chan bool
	wg            sync.WaitGroup
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

// ConnectTelnet connects to the Telnet server
func (handler *TelnetHandler) ConnectTelnet() (net.Conn, error) {
	log.Printf("Connecting to Telnet server: %s", handler.telnetAddress)
	return net.Dial("tcp", handler.telnetAddress)
}

// Start begins communication between WebSocket and Telnet
func (handler *TelnetHandler) Start() {
	handler.stopSignal = make(chan bool)

	// Start listening for WebSocket messages (commands to send to Telnet)
	handler.wg.Add(1)
	go handler.listenWebSocketMessages()

	// Continuously try to connect to Telnet and forward its data
	for {
		select {
		case <-handler.stopSignal:
			return
		default:
			telnetConn, err := handler.ConnectTelnet()
			if err != nil {
				log.Printf("Error connecting to Telnet server: %v. Retrying in %v...", err, retryInterval)
				time.Sleep(retryInterval)
				continue
			}

			// Handle Telnet communication
			handler.handleTelnetCommunication(telnetConn)
		}
	}
}

// Stop shuts down the TelnetHandler gracefully
func (handler *TelnetHandler) Stop() {
	close(handler.stopSignal)
	handler.wg.Wait()
	_ = handler.websocketConn.Close()
	log.Println("Service stopped successfully.")
}

// Listen for WebSocket messages that will be sent to the Telnet server
func (handler *TelnetHandler) listenWebSocketMessages() {
	defer handler.wg.Done()

	for {
		select {
		case <-handler.stopSignal:
			return
		default:
			// Read command from WebSocket
			_, message, err := handler.websocketConn.ReadMessage()
			if err != nil {
				log.Printf("Error reading message from WebSocket: %v", err)
				handler.stopSignal <- true
				return
			}

			command := strings.TrimSpace(string(message))
			log.Printf("Received command from WebSocket: %s", command)
		}
	}
}

// Handle Telnet communication: read data and send back to WebSocket
func (handler *TelnetHandler) handleTelnetCommunication(telnetConn net.Conn) {
	defer telnetConn.Close()

	log.Println("Connected to Telnet server. Forwarding data...")
	reader := bufio.NewReader(telnetConn)

	done := make(chan bool)
	defer close(done)

	// Read data from Telnet and send to WebSocket
	go func() {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				log.Printf("Telnet connection closed: %v", err)
				done <- true
				return
			}
			log.Printf("Received from Telnet: %s", line)

			// Send data to WebSocket
			if err := handler.websocketConn.WriteMessage(websocket.TextMessage, []byte(line)); err != nil {
				log.Printf("Error sending data to WebSocket: %v", err)
				done <- true
				return
			}
		}
	}()

	// Wait for stop signal or Telnet disconnection
	select {
	case <-handler.stopSignal:
		log.Println("Stopping Telnet communication...")
		return
	case <-done:
		log.Println("Telnet connection lost. Will retry...")
		return
	}
}

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("Usage: %s <telnet-address>", os.Args[0])
	}

	telnetAddress := os.Args[1]
	channelName := strings.ReplaceAll(telnetAddress, ":", "-") // Example: Replace ":" with "-" for channel name

	// Connect to WebSocket server
	websocketConn, err := ConnectWebSocket(channelName)
	if err != nil {
		log.Fatalf("Failed to connect to WebSocket server: %v", err)
	}
	log.Printf("Successfully connected to WebSocket channel: %s", channelName)

	// Start Telnet and WebSocket handler
	handler := &TelnetHandler{
		telnetAddress: telnetAddress,
		websocketConn: websocketConn,
	}

	go handler.Start()

	// Graceful shutdown
	stop := make(chan os.Signal, 1)
	<-stop
	handler.Stop()
}
