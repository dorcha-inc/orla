// Package core implements the core functionality for orla that is shared across all components.
package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/jonboulle/clockwork"
	"go.uber.org/zap"
)

const (
	DefaultCapsuleStartupTimeoutMs = 5000
)

// CapsuleState represents the lifecycle state of a capsule
type CapsuleState string

const (
	CapsuleStateCreated   CapsuleState = "CREATED"
	CapsuleStateStarting  CapsuleState = "STARTING"
	CapsuleStateReady     CapsuleState = "READY"
	CapsuleStateReloading CapsuleState = "RELOADING"
	CapsuleStateCrashed   CapsuleState = "CRASHED"
	CapsuleStateStopped   CapsuleState = "STOPPED"
)

// CapsuleManager manages the lifecycle of a capsule-mode tool
type CapsuleManager struct {
	tool           *ToolManifest
	state          CapsuleState
	stateMu        sync.RWMutex
	process        *exec.Cmd
	processMu      sync.RWMutex
	startupTimeout time.Duration
	clock          clockwork.Clock
	handshakeCh    chan *OrlaHelloNotification
	ctx            context.Context
	cancel         context.CancelFunc

	// JSON-RPC communication
	stdin          io.WriteCloser // Stdin pipe for sending requests
	stdout         io.ReadCloser  // Stdout pipe for reading responses
	requestID      int64          // Counter for JSON-RPC request IDs
	requestIDMu    sync.Mutex
	responses      map[int64]chan *JSONRPCResponse // Map of request ID to response channel
	responsesMu    sync.RWMutex
	responseReader *json.Decoder // JSON decoder for reading responses
}

// OrlaHelloNotification represents the orla.hello handshake notification
// as defined in RFC 3 Section 5.3.1
type OrlaHelloNotification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  OrlaHelloParams `json:"params"`
}

// OrlaHelloParams represents the parameters of the orla.hello notification
type OrlaHelloParams struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Capabilities []string `json:"capabilities"`
}

// NewCapsuleManager creates a new capsule manager for a tool
func NewCapsuleManager(tool *ToolManifest) *CapsuleManager {
	return NewCapsuleManagerWithClock(tool, clockwork.NewRealClock())
}

// NewCapsuleManagerWithClock creates a new capsule manager for a tool with a custom clock
// This is useful for testing with a fake clock
func NewCapsuleManagerWithClock(tool *ToolManifest, clock clockwork.Clock) *CapsuleManager {
	startupTimeout := DefaultCapsuleStartupTimeoutMs * time.Millisecond
	if tool.Runtime != nil && tool.Runtime.StartupTimeoutMs > 0 {
		startupTimeout = time.Duration(tool.Runtime.StartupTimeoutMs) * time.Millisecond
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &CapsuleManager{
		tool:           tool,
		state:          CapsuleStateCreated,
		startupTimeout: startupTimeout,
		clock:          clock,
		handshakeCh:    make(chan *OrlaHelloNotification, 1),
		ctx:            ctx,
		cancel:         cancel,
		responses:      make(map[int64]chan *JSONRPCResponse),
	}
}

// Start starts the capsule process and waits for the handshake
func (cm *CapsuleManager) Start() error {
	cm.stateMu.Lock()

	validStartStates := []CapsuleState{CapsuleStateCreated, CapsuleStateCrashed, CapsuleStateStopped}
	if !slices.Contains(validStartStates, cm.state) {
		cm.stateMu.Unlock()
		return fmt.Errorf("cannot start capsule in state %s", cm.state)
	}

	cm.setStateLocked(CapsuleStateStarting)
	cm.stateMu.Unlock()

	// Build command
	var cmd *exec.Cmd
	if cm.tool.Interpreter != "" {
		args := []string{cm.tool.Path}
		if cm.tool.Runtime != nil && len(cm.tool.Runtime.Args) > 0 {
			args = append(args, cm.tool.Runtime.Args...)
		}
		cmd = exec.CommandContext(cm.ctx, cm.tool.Interpreter, args...)
	} else {
		runtimeArgs := []string{}
		if cm.tool.Runtime != nil && len(cm.tool.Runtime.Args) > 0 {
			runtimeArgs = cm.tool.Runtime.Args
		}
		cmd = exec.CommandContext(cm.ctx, cm.tool.Path, runtimeArgs...)
	}

	// Set environment variables
	if cm.tool.Runtime != nil && len(cm.tool.Runtime.Env) > 0 {
		env := cmd.Environ()
		for key, value := range cm.tool.Runtime.Env {
			env = append(env, fmt.Sprintf("%s=%s", key, value))
		}
		cmd.Env = env
	}

	// Set working directory to tool's directory (parent of entrypoint)
	// Note(jadidbourbaki): tool.Path is the absolute path to the entrypoint, so we need its parent
	// TODO(jadidbourbaki): this might not be the best thought out solution, think about what we really
	// want as our working directory here.
	if toolDir := filepath.Dir(cm.tool.Path); toolDir != "" {
		cmd.Dir = toolDir
	}

	// Capture stdin and stdout for JSON-RPC communication
	stdin, stdinErr := cmd.StdinPipe()
	if stdinErr != nil {
		cm.setState(CapsuleStateCrashed)
		return fmt.Errorf("failed to create stdin pipe: %w", stdinErr)
	}

	stdout, stdoutErr := cmd.StdoutPipe()
	if stdoutErr != nil {
		cm.setState(CapsuleStateCrashed)
		return fmt.Errorf("failed to create stdout pipe: %w", stdoutErr)
	}

	cm.processMu.Lock()
	cm.process = cmd
	cm.stdin = stdin
	cm.stdout = stdout
	cm.responseReader = json.NewDecoder(stdout)
	cm.processMu.Unlock()

	// Start process
	startErr := cmd.Start()
	if startErr != nil {
		cm.setState(CapsuleStateCrashed)
		return fmt.Errorf("failed to start capsule process: %w", startErr)
	}

	// Start response reader in background to read all JSON-RPC messages
	go cm.readResponses()

	// Wait for handshake with timeout
	select {
	case notification := <-cm.handshakeCh:
		if notification == nil {
			cm.setState(CapsuleStateCrashed)
			return fmt.Errorf("handshake read failed")
		}
		cm.setState(CapsuleStateReady)
		zap.L().Info("Capsule handshake received",
			zap.String("tool", cm.tool.Name),
			zap.String("version", notification.Params.Version),
			zap.Strings("capabilities", notification.Params.Capabilities))
		return nil
	case <-cm.clock.After(cm.startupTimeout):
		cm.setState(CapsuleStateCrashed)

		stopErr := cm.Stop()
		if stopErr != nil {
			zap.L().Error("Failed to stop capsule on timeout", zap.Error(stopErr))
		}

		return fmt.Errorf("handshake timeout after %v", cm.startupTimeout)
	case <-cm.ctx.Done():
		cm.setState(CapsuleStateStopped)
		return fmt.Errorf("capsule context cancelled")
	}
}

// JSONRPCRequest represents a JSON-RPC request
type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
}

// JSONRPCResponse represents a JSON-RPC response
type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      int64         `json:"id"`
	Result  interface{}   `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC error
type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// readResponses continuously reads JSON-RPC messages from stdout
func (cm *CapsuleManager) readResponses() {
	cm.processMu.RLock()
	decoder := cm.responseReader
	cm.processMu.RUnlock()

	if decoder == nil {
		return
	}

	for {
		// Check if context is cancelled
		select {
		case <-cm.ctx.Done():
			return
		default:
		}

		// Try to decode as either a notification or response
		var rawMessage json.RawMessage
		if err := decoder.Decode(&rawMessage); err != nil {
			if err == io.EOF {
				return
			}
			// If the pipe is closed or any other error occurs, exit the goroutine
			// This prevents infinite error loops when the pipe is closed
			zap.L().Debug("Stopping response reader", zap.Error(err))
			return
		}

		// Try to decode as notification (orla.hello)
		var notification OrlaHelloNotification
		if err := json.Unmarshal(rawMessage, &notification); err == nil {
			if notification.Method == "orla.hello" {
				select {
				case cm.handshakeCh <- &notification:
				case <-cm.ctx.Done():
					return
				}
				continue
			}
		}

		// Try to decode as response
		var response JSONRPCResponse
		if err := json.Unmarshal(rawMessage, &response); err == nil {
			if response.ID != 0 {
				cm.responsesMu.RLock()
				responseCh, ok := cm.responses[response.ID]
				cm.responsesMu.RUnlock()

				if ok {
					select {
					case responseCh <- &response:
					case <-cm.ctx.Done():
						return
					}
					// Clean up response channel
					cm.responsesMu.Lock()
					delete(cm.responses, response.ID)
					cm.responsesMu.Unlock()
				}
			}
		}
	}
}

// Stop stops the capsule process
func (cm *CapsuleManager) Stop() error {
	cm.stateMu.Lock()
	defer cm.stateMu.Unlock()

	if cm.state == CapsuleStateStopped {
		return nil
	}

	cm.cancel()

	cm.processMu.Lock()
	process := cm.process
	stdin := cm.stdin
	stdout := cm.stdout
	cm.processMu.Unlock()

	// Close pipes
	if stdin != nil {
		closeErr := stdin.Close()
		if closeErr != nil {
			zap.L().Error("Failed to close stdin pipe", zap.Error(closeErr))
		}
	}
	if stdout != nil {
		closeErr := stdout.Close()
		if closeErr != nil {
			zap.L().Error("Failed to close stdout pipe", zap.Error(closeErr))
		}
	}

	if process != nil {
		killErr := process.Process.Kill()
		if killErr != nil {
			return fmt.Errorf("failed to kill capsule process: %w", killErr)
		}

		waitErr := process.Wait()
		if waitErr != nil {
			return fmt.Errorf("failed to wait for capsule process: %w", waitErr)
		}
	}

	// Clean up response channels
	cm.responsesMu.Lock()
	for id, ch := range cm.responses {
		close(ch)
		delete(cm.responses, id)
	}
	cm.responsesMu.Unlock()

	cm.setStateLocked(CapsuleStateStopped)
	return nil
}

// GetState returns the current state of the capsule
func (cm *CapsuleManager) GetState() CapsuleState {
	cm.stateMu.RLock()
	defer cm.stateMu.RUnlock()
	return cm.state
}

// setState sets the capsule state (assumes lock is NOT held)
func (cm *CapsuleManager) setState(newState CapsuleState) {
	cm.stateMu.Lock()
	defer cm.stateMu.Unlock()
	cm.setStateLocked(newState)
}

// setStateLocked sets the capsule state (assumes lock IS held)
func (cm *CapsuleManager) setStateLocked(newState CapsuleState) {
	oldState := cm.state
	cm.state = newState
	if oldState != newState {
		zap.L().Debug("Capsule state changed",
			zap.String("tool", cm.tool.Name),
			zap.String("old_state", string(oldState)),
			zap.String("new_state", string(newState)))
	}
}

// IsReady returns true if the capsule is in READY state
func (cm *CapsuleManager) IsReady() bool {
	return cm.GetState() == CapsuleStateReady
}

// CallTool sends a JSON-RPC tools/call request to the capsule and waits for the response
func (cm *CapsuleManager) CallTool(ctx context.Context, input map[string]any) (*JSONRPCResponse, error) {
	if !cm.IsReady() {
		return nil, fmt.Errorf("capsule is not ready (state: %s)", cm.GetState())
	}

	// Generate request ID
	cm.requestIDMu.Lock()
	cm.requestID++
	requestID := cm.requestID
	cm.requestIDMu.Unlock()

	// Create response channel
	responseCh := make(chan *JSONRPCResponse, 1)
	cm.responsesMu.Lock()
	cm.responses[requestID] = responseCh
	cm.responsesMu.Unlock()

	// Build JSON-RPC request
	request := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      requestID,
		Method:  "tools/call",
		Params: map[string]any{
			"name":      cm.tool.Name,
			"arguments": input,
		},
	}

	// Send request
	cm.processMu.RLock()
	stdin := cm.stdin
	cm.processMu.RUnlock()

	if stdin == nil {
		// Clean up response channel
		cm.responsesMu.Lock()
		delete(cm.responses, requestID)
		cm.responsesMu.Unlock()
		return nil, fmt.Errorf("stdin pipe is not available")
	}

	encoder := json.NewEncoder(stdin)
	if err := encoder.Encode(request); err != nil {
		// Clean up response channel
		cm.responsesMu.Lock()
		delete(cm.responses, requestID)
		cm.responsesMu.Unlock()
		return nil, fmt.Errorf("failed to send JSON-RPC request: %w", err)
	}

	// Wait for response with context timeout
	select {
	case response := <-responseCh:
		return response, nil
	case <-ctx.Done():
		// Clean up response channel
		cm.responsesMu.Lock()
		delete(cm.responses, requestID)
		cm.responsesMu.Unlock()
		return nil, fmt.Errorf("request timeout: %w", ctx.Err())
	case <-cm.ctx.Done():
		// Clean up response channel
		cm.responsesMu.Lock()
		delete(cm.responses, requestID)
		cm.responsesMu.Unlock()
		return nil, fmt.Errorf("capsule context cancelled")
	}
}
