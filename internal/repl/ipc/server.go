package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
)

// Handler is a function that handles an IPC method call.
type Handler func(ctx context.Context, params json.RawMessage) (any, error)

// HandlerError represents an error with a specific error code.
type HandlerError struct {
	Code    string
	Message string
}

func (e *HandlerError) Error() string {
	return e.Message
}

// Server handles IPC connections and routes requests to handlers.
type Server struct {
	listener *Listener
	handlers map[string]Handler
	logger   *log.Logger
	debug    bool

	mu      sync.Mutex
	running bool
	wg      sync.WaitGroup
}

// NewServer creates a new IPC server.
func NewServer(path string, logger *log.Logger, debug bool) (*Server, error) {
	listener, err := NewListener(path)
	if err != nil {
		return nil, err
	}

	return &Server{
		listener: listener,
		handlers: make(map[string]Handler),
		logger:   logger,
		debug:    debug,
	}, nil
}

// RegisterHandler registers a handler for a method.
func (s *Server) RegisterHandler(method string, handler Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[method] = handler
}

// Start begins accepting connections.
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("server already running")
	}
	s.running = true
	s.mu.Unlock()

	s.logger.Printf("IPC server listening on %s", s.listener.Path())

	go s.acceptLoop(ctx)

	return nil
}

// Stop stops the server and closes all connections.
func (s *Server) Stop() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	s.mu.Unlock()

	// Close listener to stop accept loop
	err := s.listener.Close()

	// Wait for all connections to finish
	s.wg.Wait()

	s.logger.Println("IPC server stopped")
	return err
}

// Path returns the IPC endpoint path.
func (s *Server) Path() string {
	return s.listener.Path()
}

// acceptLoop accepts incoming connections.
func (s *Server) acceptLoop(ctx context.Context) {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			// Check if we're shutting down
			s.mu.Lock()
			running := s.running
			s.mu.Unlock()
			if !running {
				return
			}
			s.logger.Printf("IPC accept error: %v", err)
			continue
		}

		s.wg.Add(1)
		go s.handleConnection(ctx, conn)
	}
}

// handleConnection handles a single client connection.
func (s *Server) handleConnection(ctx context.Context, conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()

	if s.debug {
		s.logger.Printf("IPC client connected: %s", conn.RemoteAddr())
	}

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	for {
		// Read request line
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err != io.EOF {
				s.logger.Printf("IPC read error: %v", err)
			}
			return
		}

		// Parse request
		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			resp := NewErrorResponse("", ErrCodeInvalidRequest, fmt.Sprintf("invalid JSON: %v", err))
			s.writeResponse(writer, resp)
			continue
		}

		if s.debug {
			s.logger.Printf("IPC request: %s (id=%s)", req.Method, req.ID)
		}

		// Handle request
		resp := s.handleRequest(ctx, req)

		// Write response
		if err := s.writeResponse(writer, resp); err != nil {
			s.logger.Printf("IPC write error: %v", err)
			return
		}
	}
}

// handleRequest routes a request to the appropriate handler.
func (s *Server) handleRequest(ctx context.Context, req Request) Response {
	s.mu.Lock()
	handler, ok := s.handlers[req.Method]
	s.mu.Unlock()

	if !ok {
		return NewErrorResponse(req.ID, ErrCodeMethodNotFound,
			fmt.Sprintf("unknown method: %s", req.Method))
	}

	result, err := handler(ctx, req.Params)
	if err != nil {
		// Check if it's a handler error with a specific code
		if herr, ok := err.(*HandlerError); ok {
			return NewErrorResponse(req.ID, herr.Code, herr.Message)
		}
		return NewErrorResponse(req.ID, ErrCodeInternalError, err.Error())
	}

	resp, err := NewSuccessResponse(req.ID, result)
	if err != nil {
		return NewErrorResponse(req.ID, ErrCodeInternalError,
			fmt.Sprintf("failed to marshal response: %v", err))
	}

	return resp
}

// writeResponse writes a response to the connection.
func (s *Server) writeResponse(w *bufio.Writer, resp Response) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}

	if _, err := w.Write(data); err != nil {
		return err
	}
	if err := w.WriteByte('\n'); err != nil {
		return err
	}
	return w.Flush()
}
