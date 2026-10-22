// Package httpserve serves the Log Triage MCP server over streamable HTTP at
// /mcp, for callers with a known bearer token and from a local page or none.
package httpserve

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nebius-academy-templates/build-your-own-mcp-go-sdk/server/internal/triage"
)

// Path is where the MCP endpoint is served.
const Path = "/mcp"

// Tokens maps a bearer token to the caller it names.
type Tokens map[string]triage.Caller

// ParseTokens reads LOG_TRIAGE_TOKENS: a comma-separated list of
// token=principal or token=principal:read-only.
func ParseTokens(value string) (Tokens, error) {
	tokens := Tokens{}
	for _, entry := range strings.Split(value, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		token, principal, found := strings.Cut(entry, "=")
		name, readOnly := strings.CutSuffix(principal, ":read-only")
		if !found || token == "" || name == "" || strings.Contains(name, ":") {
			return nil, fmt.Errorf("LOG_TRIAGE_TOKENS: %q is not token=principal or token=principal:read-only", entry)
		}
		tokens[token] = triage.Caller{Name: name, ReadOnly: readOnly}
	}
	if len(tokens) == 0 {
		return nil, errors.New("LOG_TRIAGE_TOKENS names no token")
	}
	return tokens, nil
}

// CheckAddress accepts HOST:PORT with a host and a port number of at most
// 65535. Port 0 asks the system for a free port.
func CheckAddress(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err == nil && host != "" && digits(port) {
		if n, err := strconv.Atoi(port); err == nil && n <= 65535 {
			return nil
		}
	}
	return fmt.Errorf("--http must be HOST:PORT with a port up to 65535, like 127.0.0.1:8765, got %q", addr)
}

// Handler serves server at Path in stateless mode. Every request passes the
// Origin check first and the bearer check second.
func Handler(server *mcp.Server, tokens Tokens) http.Handler {
	// The handler asks for a server on every request; the one server built at
	// startup keeps the one scan store.
	mcpHandler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true},
	)
	mux := http.NewServeMux()
	mux.Handle(Path, checkOrigin(checkBearer(tokens, mcpHandler)))
	return mux
}

// Serve listens on addr until ctx ends, then stops accepting requests and
// lets the running ones finish.
func Serve(ctx context.Context, addr string, handler http.Handler) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ReadHeaderTimeout: 30 * time.Second,
		ErrorLog:          log.New(os.Stderr, "log-triage http: ", 0),
	}
	failed := make(chan error, 1)
	go func() { failed <- srv.ListenAndServe() }()
	select {
	case err := <-failed:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutdown)
}

// checkOrigin answers 403 to a request whose Origin is anything but
// http://localhost or http://127.0.0.1, with or without a port, written
// exactly so, in lower case. A request without Origin passes.
func checkOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin, sent := r.Header["Origin"]
		if sent && !localOrigin(strings.Join(origin, ",")) {
			http.Error(w, "forbidden origin", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// localOrigin compares the header as written: anything after the host other
// than a port, such as a path or a user, makes the origin foreign.
func localOrigin(origin string) bool {
	rest, isHTTP := strings.CutPrefix(origin, "http://")
	if !isHTTP {
		return false
	}
	name, port, hasPort := strings.Cut(rest, ":")
	if name != "localhost" && name != "127.0.0.1" {
		return false
	}
	return !hasPort || digits(port)
}

func digits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// checkBearer answers 401 to a request without a known bearer token, and
// passes the token's caller on in the request context.
func checkBearer(tokens Tokens, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme, token, _ := strings.Cut(r.Header.Get("Authorization"), " ")
		caller, known := tokens[token]
		if !strings.EqualFold(scheme, "Bearer") || !known {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "a known bearer token is required", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(triage.WithCaller(r.Context(), caller)))
	})
}
