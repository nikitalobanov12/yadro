package gateway

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/nikita/yadro/internal/engine"
)

// Server is the HTTP/WebSocket gateway.
type Server struct {
	engine *engine.Engine
	hub    *Hub
	server *http.Server
	addr   string
}

// NewServer creates a new gateway server.
func NewServer(eng *engine.Engine, addr string) *Server {
	s := &Server{
		engine: eng,
		hub:    NewHub(),
		addr:   addr,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/order", s.handleOrder)
	mux.HandleFunc("/order/", s.handleOrderByID)
	mux.HandleFunc("/book", s.handleBook)
	mux.HandleFunc("/stats", s.handleStats)
	mux.HandleFunc("/ws", s.handleWebSocket)
	mux.HandleFunc("/health", s.handleHealth)

	// CORS middleware
	handler := corsMiddleware(mux)

	s.server = &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	return s
}

// corsMiddleware adds CORS headers.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Start starts the gateway server.
func (s *Server) Start(ctx context.Context) error {
	// Start the WebSocket hub
	go s.hub.Run(ctx)

	// Start broadcasting engine events
	go s.broadcastEvents(ctx)

	log.Printf("gateway: listening on %s", s.addr)
	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Stop stops the gateway server.
func (s *Server) Stop(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

// broadcastEvents reads from engine output and broadcasts to WebSocket clients.
func (s *Server) broadcastEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-s.engine.OutputChannel():
			if !ok {
				return
			}
			s.hub.Broadcast(event)
		}
	}
}

// OrderRequest is the JSON request for placing an order.
type OrderRequest struct {
	UserID uint64 `json:"user_id"`
	Side   string `json:"side"`  // "bid" or "ask"
	Type   string `json:"type"`  // "limit" or "market"
	Price  int64  `json:"price"` // In atomic units
	Size   int64  `json:"size"`
}

// OrderResponse is the JSON response for an order.
type OrderResponse struct {
	Success bool            `json:"success"`
	OrderID uint64          `json:"order_id,omitempty"`
	Trades  []*engine.Trade `json:"trades,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// handleOrder handles POST /order for placing orders.
func (s *Server) handleOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req OrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeJSON(w, http.StatusBadRequest, OrderResponse{
			Success: false,
			Error:   "invalid JSON: " + err.Error(),
		})
		return
	}

	// Validate request
	if req.Size <= 0 {
		s.writeJSON(w, http.StatusBadRequest, OrderResponse{
			Success: false,
			Error:   "size must be positive",
		})
		return
	}

	// Parse side
	var side engine.Side
	switch req.Side {
	case "bid", "buy":
		side = engine.Bid
	case "ask", "sell":
		side = engine.Ask
	default:
		s.writeJSON(w, http.StatusBadRequest, OrderResponse{
			Success: false,
			Error:   "side must be 'bid' or 'ask'",
		})
		return
	}

	// Parse type
	var orderType engine.OrderType
	switch req.Type {
	case "limit", "":
		orderType = engine.Limit
		if req.Price <= 0 {
			s.writeJSON(w, http.StatusBadRequest, OrderResponse{
				Success: false,
				Error:   "price must be positive for limit orders",
			})
			return
		}
	case "market":
		orderType = engine.Market
	default:
		s.writeJSON(w, http.StatusBadRequest, OrderResponse{
			Success: false,
			Error:   "type must be 'limit' or 'market'",
		})
		return
	}

	// Create and place order
	order := &engine.Order{
		UserID: req.UserID,
		Side:   side,
		Type:   orderType,
		Price:  req.Price,
		Size:   req.Size,
	}

	resp := s.engine.PlaceOrder(order)

	if !resp.Success {
		errMsg := "order failed"
		if resp.Error != nil {
			errMsg = resp.Error.Error()
		}
		s.writeJSON(w, http.StatusInternalServerError, OrderResponse{
			Success: false,
			Error:   errMsg,
		})
		return
	}

	var orderID uint64
	if resp.Order != nil {
		orderID = resp.Order.ID
	} else if len(resp.Trades) > 0 {
		// For fully filled orders, use the buy/sell order ID from the trade
		orderID = resp.Trades[0].BuyOrderID
		if side == engine.Ask {
			orderID = resp.Trades[0].SellOrderID
		}
	}

	s.writeJSON(w, http.StatusOK, OrderResponse{
		Success: true,
		OrderID: orderID,
		Trades:  resp.Trades,
	})
}

// handleOrderByID handles DELETE /order/:id for cancelling orders.
func (s *Server) handleOrderByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract order ID from path
	idStr := r.URL.Path[len("/order/"):]
	orderID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		s.writeJSON(w, http.StatusBadRequest, OrderResponse{
			Success: false,
			Error:   "invalid order ID",
		})
		return
	}

	resp := s.engine.CancelOrder(orderID)

	if !resp.Success {
		errMsg := "cancel failed"
		if resp.Error != nil {
			errMsg = resp.Error.Error()
		}
		s.writeJSON(w, http.StatusNotFound, OrderResponse{
			Success: false,
			Error:   errMsg,
		})
		return
	}

	s.writeJSON(w, http.StatusOK, OrderResponse{
		Success: true,
		OrderID: orderID,
	})
}

// handleBook handles GET /book for order book snapshot.
func (s *Server) handleBook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse depth parameter
	depth := 20
	if d := r.URL.Query().Get("depth"); d != "" {
		if parsed, err := strconv.Atoi(d); err == nil && parsed > 0 {
			depth = parsed
		}
	}

	snapshot := s.engine.Snapshot(depth)
	s.writeJSON(w, http.StatusOK, snapshot)
}

// handleStats handles GET /stats for engine statistics.
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := s.engine.Stats()
	s.writeJSON(w, http.StatusOK, stats)
}

// handleHealth handles GET /health for health checks.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// writeJSON writes a JSON response.
func (s *Server) writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("gateway: failed to encode JSON: %v", err)
	}
}

// Hub manages WebSocket connections.
type Hub struct {
	clients    map[*Client]bool
	register   chan *Client
	unregister chan *Client
	broadcast  chan interface{}
	mu         sync.RWMutex
}

// NewHub creates a new WebSocket hub.
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan interface{}, 256),
	}
}

// Run runs the hub's main loop.
func (h *Hub) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			log.Printf("hub: client connected (%d total)", len(h.clients))
		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			log.Printf("hub: client disconnected (%d total)", len(h.clients))
		case message := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					// Client too slow, drop
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Broadcast sends a message to all connected clients.
func (h *Hub) Broadcast(msg interface{}) {
	select {
	case h.broadcast <- msg:
	default:
		log.Println("hub: broadcast channel full")
	}
}

// Client represents a WebSocket client.
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan interface{}
}
