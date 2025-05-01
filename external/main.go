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

type TelnetHandler struct {
	telnetAddress   string
	websocketConn   *websocket.Conn
	stopSignal      chan struct{}
	telnetConnected bool
	wg              sync.WaitGroup
	mu              sync.Mutex
}

func ConnectWebSocket(channel string) (*websocket.Conn, error) {
	url := websocketServerURL + "/" + channel
	log.Printf("Connecting to WebSocket server: %s", url)

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.Dial(url, nil)
	if err != nil {
		return nil, err
	}

	conn.SetReadLimit(512)
	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))

	return conn, nil
}

func (handler *TelnetHandler) sendToWebSocket(message string) {
	handler.mu.Lock()
	defer handler.mu.Unlock()

	if handler.websocketConn != nil {
		handler.websocketConn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		err := handler.websocketConn.WriteMessage(websocket.TextMessage, []byte(message))
		if err != nil {
			log.Printf("Error sending message to WebSocket: %v", err)
		}
	}
}

func (handler *TelnetHandler) Start() {
	handler.stopSignal = make(chan struct{})

	handler.wg.Add(1)
	go func() {
		defer handler.wg.Done()
		handler.handleTelnetConnections()
	}()
}

func (handler *TelnetHandler) handleTelnetConnections() {
	for {
		select {
		case <-handler.stopSignal:
			log.Println("Stopping Telnet connection retries.")
			return
		default:
			conn, err := net.DialTimeout("tcp", handler.telnetAddress, 10*time.Second)
			if err != nil {
				log.Printf("Telnet connection error: %v. Retrying in %v...", err, retryInterval)
				handler.sendToWebSocket("Telnet connection lost. Retrying...\n")
				time.Sleep(retryInterval)
				continue
			}

			log.Println("Connected to Telnet server. Forwarding data...")
			handler.telnetConnected = true

			handler.forwardTelnetData(conn)

			conn.Close()
			handler.telnetConnected = false
			log.Println("Telnet connection closed. Retrying...")

			// Add small delay before reconnecting
			time.Sleep(time.Second)
		}
	}
}

func (handler *TelnetHandler) forwardTelnetData(conn net.Conn) {
	reader := bufio.NewReader(conn)
	done := make(chan struct{})

	handler.wg.Add(1)
	go func() {
		defer handler.wg.Done()
		defer close(done)

		for {
			if err := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
				log.Printf("Failed to set read deadline: %v", err)
				return
			}

			line, err := reader.ReadString('\n')
			if err != nil {
				if !strings.Contains(err.Error(), "use of closed network connection") {
					log.Printf("Telnet read error: %v", err)
					handler.sendToWebSocket("Telnet connection error: " + err.Error() + "\n")
				}
				return
			}

			line = strings.TrimSpace(line)
			if line != "" {
				log.Printf("Telnet -> WebSocket: %s", line)
				handler.sendToWebSocket(line + "\n")
			}
		}
	}()

	handler.wg.Add(1)
	go func() {
		defer handler.wg.Done()

		for {
			select {
			case <-handler.stopSignal:
				log.Println("Stopping WebSocket -> Telnet forwarding.")
				conn.Close()
				return
			case <-done:
				log.Println("Telnet connection lost. Closing WebSocket -> Telnet forwarding.")
				return
			default:
				if err := handler.websocketConn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
					log.Printf("Failed to set WebSocket read deadline: %v", err)
					return
				}

				_, msg, err := handler.websocketConn.ReadMessage()
				if err != nil {
					if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
						log.Printf("WebSocket read error: %v", err)
						handler.sendToWebSocket("WebSocket connection error: " + err.Error() + "\n")
					}
					return
				}

				if err := conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
					log.Printf("Failed to set write deadline: %v", err)
					return
				}

				msgStr := strings.TrimSpace(string(msg))
				if msgStr != "" {
					if _, err := conn.Write(append([]byte(msgStr), '\n')); err != nil {
						log.Printf("Error sending to Telnet: %v", err)
						handler.sendToWebSocket("Failed to send command to Telnet: " + err.Error() + "\n")
						return
					}
					log.Printf("WebSocket -> Telnet: %s", msgStr)
				}
			}
		}
	}()

	<-done
}

func (handler *TelnetHandler) Stop() {
	close(handler.stopSignal)
	handler.wg.Wait()

	handler.mu.Lock()
	defer handler.mu.Unlock()

	if handler.websocketConn != nil {
		handler.websocketConn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		handler.websocketConn.Close()
		log.Println("WebSocket connection closed.")
	}

	log.Println("Service stopped successfully.")
}

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("Usage: %s <telnet-address>", os.Args[0])
	}

	telnetAddress := os.Args[1]
	channelName := strings.ReplaceAll(telnetAddress, ":", "-")

	websocketConn, err := ConnectWebSocket(channelName)
	if err != nil {
		log.Fatalf("Failed to connect to WebSocket server: %v", err)
	}
	log.Printf("Connected to WebSocket channel: %s", channelName)

	handler := &TelnetHandler{
		telnetAddress: telnetAddress,
		websocketConn: websocketConn,
	}

	handler.Start()

	stop := make(chan os.Signal, 1)
	<-stop
	handler.Stop()
}
