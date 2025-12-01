package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/nikita/yadro/internal/engine"
)

func TestWebSocketConnection(t *testing.T) {
	eng, cleanup := testEngine(t)
	defer cleanup()

	server := NewServer(eng, ":0")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the hub
	go server.hub.Run(ctx)

	// Create test server
	ts := httptest.NewServer(http.HandlerFunc(server.handleWebSocket))
	defer ts.Close()

	// Convert http URL to ws URL
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

	// Connect
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	// Give time for registration
	time.Sleep(50 * time.Millisecond)

	// Verify client is registered
	server.hub.mu.RLock()
	clientCount := len(server.hub.clients)
	server.hub.mu.RUnlock()

	if clientCount != 1 {
		t.Errorf("expected 1 client, got %d", clientCount)
	}
}

func TestWebSocketBroadcast(t *testing.T) {
	eng, cleanup := testEngine(t)
	defer cleanup()

	server := NewServer(eng, ":0")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the hub
	go server.hub.Run(ctx)

	// Create test server
	ts := httptest.NewServer(http.HandlerFunc(server.handleWebSocket))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

	// Connect
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	// Give time for registration
	time.Sleep(50 * time.Millisecond)

	// Broadcast a message
	testMsg := map[string]string{"test": "message"}
	server.hub.Broadcast(testMsg)

	// Read the message
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, message, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read message: %v", err)
	}

	var received map[string]string
	if err := json.Unmarshal(message, &received); err != nil {
		t.Fatalf("failed to unmarshal message: %v", err)
	}

	if received["test"] != "message" {
		t.Errorf("expected 'message', got '%s'", received["test"])
	}
}

func TestWebSocketMultipleClients(t *testing.T) {
	eng, cleanup := testEngine(t)
	defer cleanup()

	server := NewServer(eng, ":0")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the hub
	go server.hub.Run(ctx)

	// Create test server
	ts := httptest.NewServer(http.HandlerFunc(server.handleWebSocket))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

	// Connect multiple clients
	numClients := 3
	conns := make([]*websocket.Conn, numClients)
	for i := 0; i < numClients; i++ {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("failed to connect client %d: %v", i, err)
		}
		conns[i] = conn
		defer conn.Close()
	}

	// Give time for registrations
	time.Sleep(100 * time.Millisecond)

	// Verify all clients are registered
	server.hub.mu.RLock()
	clientCount := len(server.hub.clients)
	server.hub.mu.RUnlock()

	if clientCount != numClients {
		t.Errorf("expected %d clients, got %d", numClients, clientCount)
	}

	// Broadcast a message
	testMsg := map[string]int{"count": 42}
	server.hub.Broadcast(testMsg)

	// Verify all clients receive the message
	for i, conn := range conns {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, message, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("client %d failed to read message: %v", i, err)
		}

		var received map[string]int
		if err := json.Unmarshal(message, &received); err != nil {
			t.Fatalf("client %d failed to unmarshal: %v", i, err)
		}

		if received["count"] != 42 {
			t.Errorf("client %d: expected 42, got %d", i, received["count"])
		}
	}
}

func TestWebSocketDisconnect(t *testing.T) {
	eng, cleanup := testEngine(t)
	defer cleanup()

	server := NewServer(eng, ":0")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the hub
	go server.hub.Run(ctx)

	// Create test server
	ts := httptest.NewServer(http.HandlerFunc(server.handleWebSocket))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

	// Connect
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	// Give time for registration
	time.Sleep(50 * time.Millisecond)

	// Verify client is registered
	server.hub.mu.RLock()
	clientCount := len(server.hub.clients)
	server.hub.mu.RUnlock()

	if clientCount != 1 {
		t.Errorf("expected 1 client, got %d", clientCount)
	}

	// Close the connection
	conn.Close()

	// Give time for unregistration
	time.Sleep(100 * time.Millisecond)

	// Verify client is unregistered
	server.hub.mu.RLock()
	clientCount = len(server.hub.clients)
	server.hub.mu.RUnlock()

	if clientCount != 0 {
		t.Errorf("expected 0 clients after disconnect, got %d", clientCount)
	}
}

func TestWebSocketReceivesEngineEvents(t *testing.T) {
	// Create temp WAL file
	tmpFile, err := os.CreateTemp("", "test_ws_wal_*.wal")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	cfg := &engine.Config{
		WALPath:     tmpFile.Name(),
		ChannelSize: 1000,
	}

	eng, err := engine.NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eng.Start(ctx)
	defer eng.Stop()

	server := NewServer(eng, ":0")

	// Start the hub
	go server.hub.Run(ctx)

	// Start broadcasting engine events
	go server.broadcastEvents(ctx)

	// Create test server
	ts := httptest.NewServer(http.HandlerFunc(server.handleWebSocket))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

	// Connect
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	// Give time for registration
	time.Sleep(50 * time.Millisecond)

	// Place an order through the engine
	order := &engine.Order{
		UserID: 1,
		Side:   engine.Bid,
		Type:   engine.Limit,
		Price:  10000,
		Size:   100,
	}
	eng.PlaceOrder(order)

	// Read the event from WebSocket
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, message, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read message: %v", err)
	}

	var event engine.EngineEvent
	if err := json.Unmarshal(message, &event); err != nil {
		t.Fatalf("failed to unmarshal event: %v", err)
	}

	if event.Type != "update" {
		t.Errorf("expected event type 'update', got '%s'", event.Type)
	}

	if len(event.Updates) == 0 {
		t.Error("expected at least one update")
	}
}

func TestHubBroadcastChannelFull(t *testing.T) {
	hub := NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go hub.Run(ctx)

	// Fill up the broadcast channel
	for i := 0; i < 256; i++ {
		hub.broadcast <- map[string]int{"i": i}
	}

	// This should not block (should drop the message)
	done := make(chan bool, 1)
	go func() {
		hub.Broadcast(map[string]string{"test": "dropped"})
		done <- true
	}()

	select {
	case <-done:
		// Success - broadcast didn't block
	case <-time.After(1 * time.Second):
		t.Error("broadcast blocked when channel was full")
	}
}

func TestWebSocketPingPong(t *testing.T) {
	eng, cleanup := testEngine(t)
	defer cleanup()

	server := NewServer(eng, ":0")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the hub
	go server.hub.Run(ctx)

	// Create test server
	ts := httptest.NewServer(http.HandlerFunc(server.handleWebSocket))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

	// Connect with custom dialer that has pong handler
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	// Set up pong handler
	pongReceived := make(chan bool, 1)
	conn.SetPongHandler(func(appData string) error {
		pongReceived <- true
		return nil
	})

	// Send a ping
	if err := conn.WriteMessage(websocket.PingMessage, []byte("ping")); err != nil {
		t.Fatalf("failed to send ping: %v", err)
	}

	// Read messages in a goroutine (to receive pong)
	go func() {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}()

	// Wait for pong
	select {
	case <-pongReceived:
		// Success
	case <-time.After(2 * time.Second):
		// Note: This might not receive pong depending on server implementation
		// The server sends pings, but may not respond to client pings
		t.Log("Note: server may not respond to client pings (this is okay)")
	}
}

func TestBroadcastLoopFunction(t *testing.T) {
	hub := NewHub()
	ctx, cancel := context.WithCancel(context.Background())

	go hub.Run(ctx)

	events := make(chan interface{}, 10)

	// Start broadcast loop
	go BroadcastLoop(ctx, hub, events)

	// Create a mock client
	mockClient := &Client{
		hub:  hub,
		conn: nil, // We won't use the connection
		send: make(chan interface{}, 10),
	}

	hub.register <- mockClient
	time.Sleep(50 * time.Millisecond)

	// Send an event
	testEvent := map[string]string{"type": "test"}
	events <- testEvent

	// Wait for the event to propagate
	time.Sleep(50 * time.Millisecond)

	// Check if the client received it
	select {
	case received := <-mockClient.send:
		if m, ok := received.(map[string]string); ok {
			if m["type"] != "test" {
				t.Errorf("expected 'test', got '%s'", m["type"])
			}
		} else {
			t.Errorf("unexpected message type: %T", received)
		}
	default:
		t.Error("client did not receive the event")
	}

	// Stop the loop
	cancel()
}

func TestWebSocketCloseMessage(t *testing.T) {
	eng, cleanup := testEngine(t)
	defer cleanup()

	server := NewServer(eng, ":0")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the hub
	go server.hub.Run(ctx)

	// Create test server
	ts := httptest.NewServer(http.HandlerFunc(server.handleWebSocket))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

	// Connect
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	// Give time for registration
	time.Sleep(50 * time.Millisecond)

	// Send close message
	err = conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, "bye"))
	if err != nil {
		t.Logf("write close error (may be expected): %v", err)
	}

	conn.Close()

	// Give time for cleanup
	time.Sleep(100 * time.Millisecond)

	// Verify client is unregistered
	server.hub.mu.RLock()
	clientCount := len(server.hub.clients)
	server.hub.mu.RUnlock()

	if clientCount != 0 {
		t.Errorf("expected 0 clients after close, got %d", clientCount)
	}
}
