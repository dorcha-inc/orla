package serving

import (
	"testing"

	"github.com/dorcha-inc/orla/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSharedContext(t *testing.T) {
	ctx := NewSharedContext("server1", 100)
	require.NotNil(t, ctx)
	assert.Equal(t, "server1", ctx.ServerName)
	assert.Equal(t, 100, ctx.SyncInterval)
	assert.Equal(t, 0, ctx.LastSyncTokenCount)
	assert.NotNil(t, ctx.Messages)
	assert.Len(t, ctx.Messages, 0)
}

func TestSharedContext_AppendMessage(t *testing.T) {
	ctx := NewSharedContext("server1", 100)

	msg1 := model.Message{
		Role:    model.MessageRoleUser,
		Content: "Hello",
	}
	ctx.AppendMessage(msg1)

	messages := ctx.GetMessages()
	require.Len(t, messages, 1)
	assert.Equal(t, msg1, messages[0])

	msg2 := model.Message{
		Role:    model.MessageRoleAssistant,
		Content: "Hi there",
	}
	ctx.AppendMessage(msg2)

	messages = ctx.GetMessages()
	require.Len(t, messages, 2)
	assert.Equal(t, msg1, messages[0])
	assert.Equal(t, msg2, messages[1])
}

func TestSharedContext_GetMessages_ReturnsCopy(t *testing.T) {
	ctx := NewSharedContext("server1", 100)

	msg := model.Message{
		Role:    model.MessageRoleUser,
		Content: "Hello",
	}
	ctx.AppendMessage(msg)

	messages1 := ctx.GetMessages()
	messages2 := ctx.GetMessages()

	// But should have same content
	assert.Equal(t, messages1, messages2)

	// Modifying one shouldn't affect the other (proves they're copies)
	messages1[0].Content = "Modified"
	messages3 := ctx.GetMessages()
	assert.Equal(t, "Hello", messages3[0].Content)    // Original unchanged
	assert.Equal(t, "Modified", messages1[0].Content) // Modified copy
}

func TestSharedContext_UpdateSyncTokenCount(t *testing.T) {
	ctx := NewSharedContext("server1", 100)
	assert.Equal(t, 0, ctx.LastSyncTokenCount)

	ctx.UpdateSyncTokenCount(50)
	assert.Equal(t, 50, ctx.LastSyncTokenCount)

	ctx.UpdateSyncTokenCount(150)
	assert.Equal(t, 150, ctx.LastSyncTokenCount)
}

func TestSharedContext_ShouldSync(t *testing.T) {
	ctx := NewSharedContext("server1", 100)
	ctx.UpdateSyncTokenCount(0)

	// Should not sync if delta is less than interval
	assert.False(t, ctx.ShouldSync(50))
	assert.False(t, ctx.ShouldSync(99))

	// Should sync if delta equals interval
	assert.True(t, ctx.ShouldSync(100))

	// Should sync if delta exceeds interval
	assert.True(t, ctx.ShouldSync(150))
	assert.True(t, ctx.ShouldSync(200))

	// Update token count and check again
	ctx.UpdateSyncTokenCount(100)
	assert.False(t, ctx.ShouldSync(150)) // 50 < 100
	assert.True(t, ctx.ShouldSync(200))  // 100 >= 100
}

func TestSharedContext_ShouldSync_ZeroInterval(t *testing.T) {
	ctx := NewSharedContext("server1", 0)
	ctx.UpdateSyncTokenCount(0)

	// With zero interval, should never sync
	assert.False(t, ctx.ShouldSync(100))
	assert.False(t, ctx.ShouldSync(1000))
}

func TestSharedContext_ShouldSync_NegativeInterval(t *testing.T) {
	ctx := NewSharedContext("server1", -10)
	ctx.UpdateSyncTokenCount(0)

	// With negative interval, should never sync
	assert.False(t, ctx.ShouldSync(100))
}

func TestNewContextManager(t *testing.T) {
	cm := NewContextManager()
	require.NotNil(t, cm)
	assert.NotNil(t, cm.contexts)
}

func TestContextManager_GetOrCreateSharedContext(t *testing.T) {
	cm := NewContextManager()

	// First call should create new context
	ctx1 := cm.GetOrCreateSharedContext("server1", 100)
	require.NotNil(t, ctx1)
	assert.Equal(t, "server1", ctx1.ServerName)
	assert.Equal(t, 100, ctx1.SyncInterval)

	// Second call with same server should return same context
	ctx2 := cm.GetOrCreateSharedContext("server1", 100)
	assert.Equal(t, ctx1, ctx2)

	// Different server should create new context
	ctx3 := cm.GetOrCreateSharedContext("server2", 200)
	require.NotNil(t, ctx3)
	assert.Equal(t, "server2", ctx3.ServerName)
	assert.Equal(t, 200, ctx3.SyncInterval)
	assert.NotEqual(t, ctx1, ctx3)
}

func TestContextManager_GetOrCreateSharedContext_DifferentSyncInterval(t *testing.T) {
	cm := NewContextManager()

	// Create context with sync interval 100
	ctx1 := cm.GetOrCreateSharedContext("server1", 100)
	assert.Equal(t, 100, ctx1.SyncInterval)

	// Getting with different sync interval should return same context (interval not updated)
	ctx2 := cm.GetOrCreateSharedContext("server1", 200)
	assert.Equal(t, ctx1, ctx2)
	assert.Equal(t, 100, ctx2.SyncInterval) // Original interval preserved
}

func TestContextManager_GetSharedContext(t *testing.T) {
	cm := NewContextManager()

	// Getting non-existent context should return nil
	ctx := cm.GetSharedContext("server1")
	assert.Nil(t, ctx)

	// Create context
	createdCtx := cm.GetOrCreateSharedContext("server1", 100)
	require.NotNil(t, createdCtx)

	// Now should be able to get it
	ctx = cm.GetSharedContext("server1")
	assert.Equal(t, createdCtx, ctx)
}

func TestContextManager_ConcurrentAccess(t *testing.T) {
	cm := NewContextManager()

	// Test concurrent access to GetOrCreateSharedContext
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			ctx := cm.GetOrCreateSharedContext("server1", 100)
			require.NotNil(t, ctx)
			ctx.AppendMessage(model.Message{
				Role:    model.MessageRoleUser,
				Content: "test",
			})
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should have all messages
	ctx := cm.GetSharedContext("server1")
	require.NotNil(t, ctx)
	messages := ctx.GetMessages()
	assert.Len(t, messages, 10)
}
