package node

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDisconnectQueue_Run(t *testing.T) {
	t.Run("Disconnects sessions", func(t *testing.T) {
		q := newQueue()
		defer q.Shutdown()

		q.Enqueue(NewMockSession("1"))
		assert.Equal(t, 1, q.Size())

		go func() {
			q.Run()
		}()

		// TODO: We need a trully node mock to control disconnect operation
		for q.Size() > 0 {
			runtime.Gosched()
		}
	})
}

func TestDisconnectQueue_Shutdown(t *testing.T) {
	t.Run("Disconnects sessions", func(t *testing.T) {
		q := newQueue()

		q.Enqueue(NewMockSession("1"))
		q.Enqueue(NewMockSession("2"))
		assert.Equal(t, 2, q.Size())

		q.Shutdown()
		assert.Equal(t, 0, q.Size())
	})

	t.Run("Stops after timeout", func(t *testing.T) {
		t.Skip("TODO: We need a trully node mock to control disconnect operation")
	})

	t.Run("Allows multiple entering", func(t *testing.T) {
		q := newQueue()

		for i := 1; i <= 10; i++ {
			q.Shutdown()
		}
	})
}

func TestDisconnectQueue_Enqueue(t *testing.T) {
	t.Run("Adds sessions to the queue", func(t *testing.T) {
		q := newQueue()

		q.Enqueue(NewMockSession("1"))
		assert.Equal(t, 1, q.Size())
	})

	t.Run("After shutdown", func(t *testing.T) {
		q := newQueue()
		q.Shutdown()

		q.Enqueue(NewMockSession("1"))
		assert.Equal(t, 0, q.Size())
	})
}

func newQueue() *DisconnectQueue {
	node := NewMockNode()
	config := NewDisconnectQueueConfig()
	config.Rate = 1
	q := NewDisconnectQueue(&node, &config)

	return q
}
