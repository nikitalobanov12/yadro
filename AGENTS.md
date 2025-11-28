# AGENTS.md - YADRO Limit Order Book Engine

## Build & Test Commands
```bash
go build -o yadro ./cmd/yadro           # Build the engine
go test ./...                            # Run all tests
go test -v ./internal/engine -run TestMatcherLimitOrderFullMatch  # Run single test
go test -bench=BenchmarkMatchOrder ./internal/engine -benchtime=100ms  # Benchmarks
./yadro -addr :8080 -wal yadro.wal      # Run server
```

## Code Style
- **No floats**: Use `int64` for Price/Quantity (atomic units like cents)
- **Imports**: stdlib first, blank line, external (`gorilla/websocket`), blank line, internal
- **Errors**: Wrap with `%w`, use `errors.Is`/`errors.As`, no panics in libs
- **Naming**: `orderBook` not `ob`, `matchOrder` not `match`, descriptive names
- **Context**: Pass `context.Context` first for I/O and cancellation

## Architecture
- Layout: `cmd/yadro/`, `internal/engine/`, `internal/wal/`, `internal/gateway/`
- Engine is single-threaded event loop, WAL write before ack, channel-based I/O
- Red-Black Tree (`rbtree.go`) for O(log n) price level operations
- WebSocket Hub broadcasts delta updates (not full book) to connected clients

## API Endpoints
- `POST /order` - Place order (JSON: `{user_id, side, type, price, size}`)
- `DELETE /order/:id` - Cancel order
- `GET /book?depth=20` - Order book snapshot
- `GET /ws` - WebSocket for real-time updates
