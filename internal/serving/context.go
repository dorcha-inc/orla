// Package serving implements the Agentic Serving Layer (RFC 5).
package serving

import (
	"sync"

	"github.com/dorcha-inc/orla/internal/model"
)

// SharedContext manages shared conversation context for multi-agent scenarios
type SharedContext struct {
	// ServerName is the name of the LLM server this context belongs to
	ServerName string
	// Messages is the shared conversation history
	Messages []model.Message
	// mu protects access to Messages
	mu sync.RWMutex
	// SyncInterval is how often to synchronize context (in tokens)
	SyncInterval int
	// LastSyncTokenCount is the token count at the last synchronization
	LastSyncTokenCount int
}

// NewSharedContext creates a new shared context
func NewSharedContext(serverName string, syncInterval int) *SharedContext {
	return &SharedContext{
		ServerName:         serverName,
		Messages:           make([]model.Message, 0),
		SyncInterval:       syncInterval,
		LastSyncTokenCount: 0,
	}
}

// AppendMessage appends a message to the shared context
func (sc *SharedContext) AppendMessage(msg model.Message) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.Messages = append(sc.Messages, msg)
}

// GetMessages returns a copy of all messages in the shared context
func (sc *SharedContext) GetMessages() []model.Message {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	messages := make([]model.Message, len(sc.Messages))
	copy(messages, sc.Messages)
	return messages
}

// UpdateSyncTokenCount updates the token count for synchronization tracking
func (sc *SharedContext) UpdateSyncTokenCount(tokenCount int) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.LastSyncTokenCount = tokenCount
}

// ShouldSync checks if the context should be synchronized based on token count
func (sc *SharedContext) ShouldSync(currentTokenCount int) bool {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	if sc.SyncInterval <= 0 {
		return false // No sync interval configured
	}
	delta := currentTokenCount - sc.LastSyncTokenCount
	return delta >= sc.SyncInterval
}

// ContextManager coordinates context sharing across agents
type ContextManager struct {
	// contexts maps LLM server names to their shared contexts
	contexts map[string]*SharedContext
	// mu protects access to contexts
	mu sync.RWMutex
}

// NewContextManager creates a new context manager
func NewContextManager() *ContextManager {
	return &ContextManager{
		contexts: make(map[string]*SharedContext),
	}
}

// GetOrCreateSharedContext gets or creates a shared context for an LLM server
func (cm *ContextManager) GetOrCreateSharedContext(serverName string, syncInterval int) *SharedContext {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if ctx, exists := cm.contexts[serverName]; exists {
		return ctx
	}

	ctx := NewSharedContext(serverName, syncInterval)
	cm.contexts[serverName] = ctx
	return ctx
}

// GetSharedContext gets a shared context for an LLM server (returns nil if not found)
func (cm *ContextManager) GetSharedContext(serverName string) *SharedContext {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.contexts[serverName]
}
