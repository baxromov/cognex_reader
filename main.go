package main

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
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
			stopSignal:  make(chan bool),
		}
		channels[channelName] = channel
		go handleTelnetConnection(channel, channelName)
	} else {
		select {
		case <-channel.stopSignal:
			channel.stopSignal = make(chan bool)
			go handleTelnetConnection(channel, channelName)
		default:
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

// Generate a self-signed SSL certificate dynamically
func generateSelfSignedCertificate() (string, string, error) {
	// Generate private key
	privateKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return "", "", err
	}

	// Create certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Self-Signed Org"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour), // 1 year
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	// Generate self-signed certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return "", "", err
	}

	// Save temp cert file
	certFile, err := os.CreateTemp("", "cert.pem")
	if err != nil {
		return "", "", err
	}
	defer certFile.Close()

	if err := pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		return "", "", err
	}

	// Save temp key file
	keyFile, err := os.CreateTemp("", "key.pem")
	if err != nil {
		return "", "", err
	}
	defer keyFile.Close()

	privateKeyBytes, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return "", "", err
	}

	if err := pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: privateKeyBytes}); err != nil {
		return "", "", err
	}
	return certFile.Name(), keyFile.Name(), nil
}

func main() {
	// Generate self-signed certificate and key
	certFile, keyFile, err := generateSelfSignedCertificate()
	if err != nil {
		log.Fatalf("Failed to generate SSL certificate: %v", err)
	}
	defer os.Remove(certFile)
	defer os.Remove(keyFile)

	// Register HTTP handlers
	http.HandleFunc("/", websocketHandler)

	// Start insecure WS (HTTP)
	go func() {
		log.Println("Starting WebSocket server on ws://localhost:5001")
		if err := http.ListenAndServe(":5001", nil); err != nil {
			log.Fatalf("HTTP Server Error: %s", err)
		}
	}()

	// Start secure WSS (HTTPS)
	log.Println("Starting Secure WebSocket server on wss://localhost:5002")
	if err := http.ListenAndServeTLS(":5002", certFile, keyFile, nil); err != nil {
		log.Fatalf("HTTPS Server Error: %s", err)
	}
}
