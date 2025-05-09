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
	telnetAddress string
	websocketConn *websocket.Conn
	logsConn      *websocket.Conn
	stopSignal    chan struct{}
	wg            sync.WaitGroup
	dataMu        sync.Mutex
	logMu         sync.Mutex
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

func (handler *TelnetHandler) sendToWebSocket(message WebSocketMessage) {
	jsonMsg, err := json.Marshal(message)
	if err != nil {
		log.Printf("Failed to marshal WebSocketMessage: %v", err)
		return
	}

	handler.dataMu.Lock()
	defer handler.dataMu.Unlock()

	if handler.websocketConn != nil {
		handler.websocketConn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		err := handler.websocketConn.WriteMessage(websocket.TextMessage, jsonMsg)
		if err != nil {
			log.Printf("WebSocket write failed: %v", err)
			handler.reconnectWebSocket("data")
		}
	} else {
		log.Println("WebSocket is disconnected. Telnet data continues to be processed.")
	}
}

func (handler *TelnetHandler) reconnectWebSocket(connType string) {
	var err error
	channelName := strings.ReplaceAll(handler.telnetAddress, ":", "-")

	for attempts := 0; attempts < 3; attempts++ {
		if connType == "data" {
			handler.dataMu.Lock()
			if handler.websocketConn != nil {
				handler.websocketConn.Close()
			}
			handler.websocketConn, err = ConnectWebSocket(channelName)
			handler.dataMu.Unlock()

		} else if connType == "logs" {
			handler.logMu.Lock()
			if handler.logsConn != nil {
				handler.logsConn.Close()
			}
			handler.logsConn, err = ConnectWebSocket(channelName + "-logs")
			handler.logMu.Unlock()
		}

		if err == nil {
			log.Printf("Reconnected to WebSocket [%s] successfully", connType)
			return
		}

		log.Printf("WebSocket reconnection attempt [%s] failed: %v. Retrying in 2 seconds...", connType, err)
		time.Sleep(2 * time.Second)
	}

	log.Printf("Failed to reconnect to WebSocket [%s] after 3 attempts.", connType)
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
			handler.forwardTelnetData(conn)
			conn.Close()
			log.Println("Telnet connection closed. Retrying...")
			time.Sleep(1 * time.Second)
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
				log.Printf("Failed to set read deadline on Telnet connection: %v", err)
				handler.sendToWebSocket(WebSocketMessage{Error: "Telnet timeout"})
				return
			}

			line, err := reader.ReadString('\n')
			if err != nil {
				log.Printf("Error reading from Telnet: %v", err)
				handler.sendToWebSocket(WebSocketMessage{Error: "Telnet read error: " + err.Error()})
				return
			}

			line = strings.TrimSpace(line)
			if line != "" {
				log.Printf("Telnet -> WebSocket Data: %s", line)
				handler.sendToWebSocket(WebSocketMessage{Data: line})
			}
		}
	}()

	<-done
}

func (handler *TelnetHandler) Start() {
	handler.stopSignal = make(chan struct{})

	handler.wg.Add(1)
	go func() {
		defer handler.wg.Done()
		handler.handleTelnetConnections()
	}()
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
