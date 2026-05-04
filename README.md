# [enter]prize

`prize` is a Go package that provides a type-safe, generic HTTP handler with built-in middleware support. Built on top of [`github.com/4thPlanet/dispatch`](https://github.com/4thPlanet/dispatch), it wires together session management, request logging, error handling, and content encoding into a single composable handler — so you can focus on your application logic rather than plumbing.

## Features

- **Generic, type-safe request data** — bind strongly-typed request context (`T`) to each incoming request via a user-supplied factory function
- **Typed Handlers** — Generic `TypedHandler[S, T]` for type-safe request handling with session and custom data support
- **Session Management** — pluggable `SessionStore` with a configurable initializer; defaults to an in-memory session store
- **Request Logging** — Apache-style access logging with configurable format directives
- **Error Handling** — centralized, swappable error handler with panic recovery and stack trace logging
- **Content Encoding** — support for gzip, deflate, and custom encoders with Content-Type negotiation
- **Middleware chaining** — attach custom middleware that runs after the built-in layers
- **WebSocket support** — indirect, via the `gobwas/ws` dependency pulled in through `dispatch`

## Requirements

Go 1.26.1 or later

## Installation

```bash
go get github.com/4thPlanet/prize
```

## Quick Start

```go
package main

import (
    "fmt"
    "net/http"

    "github.com/4thPlanet/prize"
    "github.com/4thPlanet/prize/middleware"
)

// Define your session type. Must implement middleware.Session interface (Id() string, Load(*http.Request), Save(*http.Request))
type MySession struct {
    UserID string
}

func (s MySession) Id() string { return s.UserID }

// Define your per-request data type.
type RequestData struct {
    Name string
}

func main() {
    // Create a typed handler.
    // The first argument is a session initializer; the second extracts typed data from the request.
    handler := prize.NewTypedHandler[MySession](
        func() MySession { return MySession{} },
        func(r *http.Request) RequestData {
            return RequestData{Name: r.URL.Query().Get("name")}
        },
    )

    // Register routes - HandlerData gives access to Session and RequestData
    handler.HandleFunc("GET /hello", func(w http.ResponseWriter, hd *prize.HandlerData[MySession, RequestData]) {
        fmt.Fprintf(w, "Hello, %s!", hd.Data.Name)
    })

    // Register custom middleware (logging, error handling, sessions, and content encoding
    // are automatically prepended).
    handler.UseMiddleware(/* your custom middleware here */)

    http.ListenAndServe(":8080", handler)
}
```

A more detailed example — including session usage, content negotiation, and authentication — is available in [`examples/basic`](examples/basic).

## API Reference

### `TypedHandler[S, T]`

The central type. `S` must satisfy `middleware.Session`; `T` is your arbitrary per-request data struct.

```go
type TypedHandler[S middleware.Session, T any] struct { ... }
```

#### `NewTypedHandler`

```go
func NewTypedHandler[S middleware.Session, T any](
    sessionInit func() S,
    fn func(*http.Request) T,
) *TypedHandler[S, T]
```

Creates a new handler. `sessionInit` produces a blank session value; `fn` builds the typed request data from the incoming `*http.Request`.

#### `UseLog`

```go
func (mux *TypedHandler[S, T]) UseLog(format string, l io.Writer)
```

Override the log format string and output destination. The default format is Apache Combined Log Format (`%h %l %u %t "%r" %s %b`); the default writer is `log.Default().Writer()`.

#### `UseErrorHandler`

```go
func (mux *TypedHandler[S, T]) UseErrorHandler(
    handler middleware.ErrorHandler[*HandlerData[S, T]],
    ctn *dispatch.ContentTypeNegotiator,
)
```

Swap in a custom error handler and content-type negotiator.

#### `UseContentEncoders`

```go
func (mux *TypedHandler[S, T]) UseContentEncoders(encs ...middleware.ContentEncoder)
```

Register one or more response content encoders (e.g. gzip).

#### `UseSessionStore`

```go
func (mux *TypedHandler[S, T]) UseSessionStore(store middleware.SessionStore[S], init func() S)
```

Replace the default in-memory session store.

#### `UseMiddleware`

```go
func (mux *TypedHandler[S, T]) UseMiddleware(mws ...dispatch.Middleware[*HandlerData[S, T]])
```

Attach custom middleware. The following built-in middleware layers are automatically prepended in order:

1. **Logger** — logs each request
2. **Error handler** — catches and formats errors
3. **Session** — loads/saves the session for each request
4. **Content encoding** — applies response encoders

### `HandlerData[S, T]`

The context object passed to every route handler. Provides access to the raw request, the typed data, and the loaded session.

```go
type HandlerData[S middleware.Session, T any] struct {
    Data    T
    Session S
    // contains unexported fields
}

func (d *HandlerData[S, T]) Request() *http.Request
func (d *HandlerData[S, T]) LoadSession(session S)
```

## Middleware

### Logger

Apache-style access logging with support for standard directives:
- `%h` - Remote host
- `%l` - Remote logname
- `%u` - Remote user
- `%t` - Time
- `%r` - Request line
- `%s` - Status code
- `%b` - Response size
- `%D` - Request duration (microseconds)
- `%T` - Request duration (seconds)
- `%{format}t` - Custom time format
- `%{Header}i` / `%{Header}o` - Request/response headers
- `%m` - Request method
- `%U` - Request URL path
- `%q` - Query string
- `%p` - Local port
- `%H` - Request protocol
- And more...

### Error Handling

Catches panics and returns configurable error responses with stack trace logging. Supports content type negotiation for error responses.

### Session

Generic session management with:
- `Session` interface for custom session types (requires `Id() string`)
- `DefaultSessionStore` for in-memory session storage
- Cookie-based session tracking
- Configurable session lifecycle

### Content Encoding

Content encoding support:
- `gzip` and `deflate` compression
- Custom encoder support via `ContentEncoder` interface
- Automatic `Accept-Encoding` negotiation

## Running the Example

```bash
go run examples/basic/main.go
```

The example server starts on `localhost:8080` with:
- `GET /` - List users (supports HTML, JSON, CSV content negotiation)
- `POST /` - Add a new user (requires Basic Authentication)
- `GET /panic` - Test panic recovery middleware

## Building & Testing

```bash
# Build
go build ./...

# Run tests
go test ./...

# Run tests with verbose output
go test -v ./...
```

## Dependencies

| Module | Purpose |
|---|---|
| `github.com/4thPlanet/dispatch` | Typed HTTP routing and middleware chaining |
| `github.com/gobwas/ws` | WebSocket support (indirect) |
| `golang.org/x/sys` | System-level support (indirect) |

## License

BSD 3-Clause License. See [LICENSE](LICENSE) for details.
