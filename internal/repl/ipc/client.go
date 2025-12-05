package ipc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Client is an IPC client for communicating with the steep-repl daemon.
type Client struct {
	conn   net.Conn
	reader *bufio.Reader
	writer *bufio.Writer
	mu     sync.Mutex
}

// NewClient creates a new IPC client connected to the daemon.
func NewClient(path string) (*Client, error) {
	if path == "" {
		path = DefaultSocketPath()
	}

	conn, err := Dial(path)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to IPC socket: %w", err)
	}

	return &Client{
		conn:   conn,
		reader: bufio.NewReader(conn),
		writer: bufio.NewWriter(conn),
	}, nil
}

// Close closes the client connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// Call makes an IPC call and returns the response.
func (c *Client) Call(method string, params any) (*Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Build request
	id := uuid.New().String()
	req := Request{
		ID:     id,
		Method: method,
	}

	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal params: %w", err)
		}
		req.Params = data
	}

	// Set deadline
	if err := c.conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return nil, fmt.Errorf("failed to set deadline: %w", err)
	}

	// Send request
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	if _, err := c.writer.Write(data); err != nil {
		return nil, fmt.Errorf("failed to write request: %w", err)
	}
	if err := c.writer.WriteByte('\n'); err != nil {
		return nil, fmt.Errorf("failed to write newline: %w", err)
	}
	if err := c.writer.Flush(); err != nil {
		return nil, fmt.Errorf("failed to flush: %w", err)
	}

	// Read response
	line, err := c.reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &resp, nil
}

// GetStatus calls status.get and returns the result.
func (c *Client) GetStatus() (*StatusResult, error) {
	resp, err := c.Call(MethodStatusGet, nil)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
	}

	var result StatusResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to parse result: %w", err)
	}

	return &result, nil
}

// HealthCheck calls health.check and returns the result.
func (c *Client) HealthCheck() (*HealthCheckResult, error) {
	resp, err := c.Call(MethodHealthCheck, nil)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
	}

	var result HealthCheckResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to parse result: %w", err)
	}

	return &result, nil
}

// ListNodes calls nodes.list and returns the result.
func (c *Client) ListNodes(statusFilter []string) (*NodesListResult, error) {
	params := NodesListParams{StatusFilter: statusFilter}
	resp, err := c.Call(MethodNodesList, params)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
	}

	var result NodesListResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to parse result: %w", err)
	}

	return &result, nil
}

// GetNode calls nodes.get and returns the result.
func (c *Client) GetNode(nodeID string) (*NodesGetResult, error) {
	params := NodesGetParams{NodeID: nodeID}
	resp, err := c.Call(MethodNodesGet, params)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
	}

	var result NodesGetResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to parse result: %w", err)
	}

	return &result, nil
}

// QueryAudit calls audit.query and returns the result.
func (c *Client) QueryAudit(params AuditQueryParams) (*AuditQueryResult, error) {
	resp, err := c.Call(MethodAuditQuery, params)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
	}

	var result AuditQueryResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to parse result: %w", err)
	}

	return &result, nil
}

// CancelInit calls init.cancel and returns the result.
func (c *Client) CancelInit(nodeID string) (*InitCancelResult, error) {
	params := InitCancelParams{NodeID: nodeID}
	resp, err := c.Call(MethodInitCancel, params)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
	}

	var result InitCancelResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to parse result: %w", err)
	}

	return &result, nil
}
