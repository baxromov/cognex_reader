package main

import (
	"bufio"
	"encoding/json"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const websocketServerURL = "wss://laundirs-supply-chain-websocket.azurewebsites.net"

var retryInterval = 5 * time.Second

type WebSocketMessage struct {
	Data  string `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

type TelnetHandler struct {
	telnetAddress   string
	websocketConn   *websocket.Conn
	logsConn        *websocket.Conn
	stopSignal      chan struct{}
	telnetConnected bool
	wg              sync.WaitGroup
	dataMu          sync.Mutex
	logMu           sync.Mutex
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

	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	})
	go func(c *websocket.Conn) {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			<-ticker.C
			c.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("Ping error: %v", err)
				return
			}
		}
	}(conn)
	return conn, nil
}

func (handler *TelnetHandler) sendError(errMsg string) {
	handler.logMu.Lock()
	defer handler.logMu.Unlock()

	if handler.logsConn != nil {
		handler.logsConn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		jsonMsg, err := json.Marshal(WebSocketMessage{Error: errMsg})
		if err != nil {
			log.Printf("Error marshaling error message: %v", err)
			return
		}
		err = handler.logsConn.WriteMessage(websocket.TextMessage, jsonMsg)
		if err != nil {
			log.Printf("Error sending error message: %v", err)
			handler.reconnectWebSocket("logs")
		}
	}
}

func (handler *TelnetHandler) sendToWebSocket(message WebSocketMessage) {
	if message.Data != "" {
		handler.dataMu.Lock()
		defer handler.dataMu.Unlock()

		if handler.websocketConn != nil {
			handler.websocketConn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			jsonMsg, err := json.Marshal(message)
			if err != nil {
				log.Printf("Error marshaling message: %v", err)
				return
			}
			err = handler.websocketConn.WriteMessage(websocket.TextMessage, jsonMsg)
			if err != nil {
				log.Printf("Error sending message to WebSocket: %v", err)
				handler.reconnectWebSocket("data")
			}
		}
	} else if message.Error != "" {
		handler.sendError(message.Error)
	}
}

func (handler *TelnetHandler) reconnectWebSocket(connType string) {
	var err error
	channelName := strings.ReplaceAll(handler.telnetAddress, ":", "-")

	if connType == "data" {
		if handler.websocketConn != nil {
			handler.websocketConn.Close()
		}
		for i := 0; i < 3; i++ {
			handler.websocketConn, err = ConnectWebSocket(channelName)
			if err == nil {
				log.Printf("Successfully reconnected to WebSocket channel: %s", channelName)
				break
			}
			log.Printf("Failed to reconnect to WebSocket, attempt %d: %v", i+1, err)
			time.Sleep(time.Second * 2)
		}
	} else if connType == "logs" {
		if handler.logsConn != nil {
			handler.logsConn.Close()
		}
		for i := 0; i < 3; i++ {
			handler.logsConn, err = ConnectWebSocket(channelName + "-logs")
			if err == nil {
				log.Printf("Successfully reconnected to logs WebSocket channel: %s-logs", channelName)
				break
			}
			log.Printf("Failed to reconnect to logs WebSocket, attempt %d: %v", i+1, err)
			time.Sleep(time.Second * 2)
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
				handler.sendToWebSocket(WebSocketMessage{Error: "Telnet connection lost. Retrying..."})
				time.Sleep(retryInterval)
				continue
			}

			log.Println("Connected to Telnet server. Forwarding data...")
			handler.telnetConnected = true

			handler.forwardTelnetData(conn)

			conn.Close()
			handler.telnetConnected = false
			log.Println("Telnet connection closed. Retrying...")

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
				handler.sendToWebSocket(WebSocketMessage{Error: "Failed to set read deadline"})
				return
			}

			line, err := reader.ReadString('\n')
			if err != nil {
				if !strings.Contains(err.Error(), "use of closed network connection") {
					log.Printf("Telnet read error: %v", err)
					handler.sendToWebSocket(WebSocketMessage{Error: "Telnet connection error: " + err.Error()})
				}
				return
			}

			line = strings.TrimSpace(line)
			if line != "" {
				log.Printf("Telnet -> WebSocket: %s", line)
				handler.sendToWebSocket(WebSocketMessage{Data: line})
			}
		}
	}()

	<-done
}

func (handler *TelnetHandler) Stop() {
	close(handler.stopSignal)
	handler.wg.Wait()

	handler.dataMu.Lock()
	if handler.websocketConn != nil {
		handler.websocketConn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		handler.websocketConn.Close()
		log.Println("WebSocket connection closed.")
	}
	handler.dataMu.Unlock()

	handler.logMu.Lock()
	if handler.logsConn != nil {
		handler.logsConn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		handler.logsConn.Close()
		log.Println("Logs WebSocket connection closed.")
	}
	handler.logMu.Unlock()

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

	logsConn, err := ConnectWebSocket(channelName + "-logs")
	if err != nil {
		log.Printf("Failed to connect to logs WebSocket: %v", err)
	} else {
		log.Printf("Connected to logs WebSocket channel: %s-logs", channelName)
	}

	handler := &TelnetHandler{
		telnetAddress: telnetAddress,
		websocketConn: websocketConn,
		logsConn:      logsConn,
	}

	handler.Start()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
	handler.Stop()
}
