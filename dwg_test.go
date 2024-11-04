package dwg

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDynamicWaitGroup_AddDone(t *testing.T) {
	tests := []struct {
		name          string
		adds          []int
		expectPanic   bool
		expectedCount int
	}{
		{
			name:          "Simple Add and Done",
			adds:          []int{1, -1},
			expectPanic:   false,
			expectedCount: 0,
		},
		{
			name:          "Multiple Adds and Dones",
			adds:          []int{3, -1, -1, -1},
			expectPanic:   false,
			expectedCount: 0,
		},
		{
			name:          "Negative Task Counter Panic",
			adds:          []int{-1},
			expectPanic:   true,
			expectedCount: 0,
		},
		{
			name:          "Done without Add Panic",
			adds:          []int{0, -1},
			expectPanic:   true,
			expectedCount: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dwg := NewDynamicWaitGroup()
			if test.expectPanic {
				assert.Panics(t, func() {
					for _, delta := range test.adds {
						if delta > 0 {
							success := dwg.Add(delta)
							assert.True(t, success)
						} else if delta < 0 {
							for i := 0; i < -delta; i++ {
								dwg.Done()
							}
						} else {
							dwg.Done()
						}
					}
				})
			} else {
				for _, delta := range test.adds {
					if delta > 0 {
						success := dwg.Add(delta)
						assert.True(t, success)
					} else if delta < 0 {
						for i := 0; i < -delta; i++ {
							dwg.Done()
						}
					} else {
						dwg.Done()
					}
				}
				count := dwg.Count()
				assert.Equal(t, test.expectedCount, count)
			}
		})
	}
}

func TestDynamicWaitGroup_Wait(t *testing.T) {
	tests := []struct {
		name      string
		taskCount int
	}{
		{
			name:      "Wait with no tasks",
			taskCount: 0,
		},
		{
			name:      "Wait with multiple tasks",
			taskCount: 5,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dwg := NewDynamicWaitGroup()
			done := make(chan struct{})
			go func() {
				dwg.Wait()
				close(done)
			}()

			if test.taskCount > 0 {
				for i := 0; i < test.taskCount; i++ {
					success := dwg.Add(1)
					require.True(t, success)
					go func() {
						time.Sleep(10 * time.Millisecond)
						dwg.Done()
					}()
				}
			}

			select {
			case <-done:
				// Success
			case <-time.After(100 * time.Millisecond):
				t.Fatal("Wait timed out")
			}
		})
	}
}

func TestDynamicWaitGroup_WaitContext(t *testing.T) {
	dwg := NewDynamicWaitGroup()
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	doneCh := make(chan error)
	go func() {
		err := dwg.WaitContext(ctx)
		doneCh <- err
	}()

	select {
	case err := <-doneCh:
		assert.Equal(t, context.DeadlineExceeded, err)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("WaitContext did not return in time")
	}
}

func TestDynamicWaitGroup_LockUnlock(t *testing.T) {
	dwg := NewDynamicWaitGroup()
	success := dwg.Add(1)
	require.True(t, success)

	lockDone := make(chan struct{})
	go func() {
		dwg.Lock()
		close(lockDone)
	}()

	// Ensure Lock is blocking
	select {
	case <-lockDone:
		t.Fatal("Lock should be blocking until tasks are done")
	case <-time.After(50 * time.Millisecond):
		// Expected
	}

	dwg.Done()

	select {
	case <-lockDone:
		// Success
	case <-time.After(50 * time.Millisecond):
		t.Fatal("Lock did not unblock after tasks are done")
	}

	// Unlock and ensure we can add tasks
	dwg.Unlock()
	success = dwg.Add(1)
	assert.True(t, success)
}

func TestDynamicWaitGroup_Shutdown(t *testing.T) {
	dwg := NewDynamicWaitGroup()
	success := dwg.Add(1)
	require.True(t, success)

	shutdownDone := make(chan struct{})
	go func() {
		dwg.Shutdown()
		close(shutdownDone)
	}()

	// Ensure Shutdown is blocking
	select {
	case <-shutdownDone:
		t.Fatal("Shutdown should be blocking until tasks are done")
	case <-time.After(50 * time.Millisecond):
		// Expected
	}

	dwg.Done()

	select {
	case <-shutdownDone:
		// Success
	case <-time.After(50 * time.Millisecond):
		t.Fatal("Shutdown did not complete after tasks are done")
	}

	// Ensure group is closed
	assert.True(t, dwg.Closed())

	// Attempt to add a task after Shutdown
	success = dwg.Add(1)
	assert.False(t, success)
}

func TestDynamicWaitGroup_Close(t *testing.T) {
	dwg := NewDynamicWaitGroup()
	success := dwg.Add(1)
	require.True(t, success)

	dwg.Close()

	// Ensure group is closed
	assert.True(t, dwg.Closed())

	// Attempt to add a task after Close
	success = dwg.Add(1)
	assert.False(t, success)

	// Attempt to call Done after Close
	assert.Panics(t, func() {
		dwg.Done()
	})
}

func TestDynamicWaitGroup_PanicOnUnlockWithoutLock(t *testing.T) {
	dwg := NewDynamicWaitGroup()
	assert.Panics(t, func() {
		dwg.Unlock()
	})
}

func TestDynamicWaitGroup_WaitContext_Cancel(t *testing.T) {
	dwg := NewDynamicWaitGroup()
	success := dwg.Add(1)
	require.True(t, success)

	ctx, cancel := context.WithCancel(context.Background())
	waitDone := make(chan error)
	go func() {
		err := dwg.WaitContext(ctx)
		waitDone <- err
	}()

	cancel()

	select {
	case err := <-waitDone:
		assert.Equal(t, context.Canceled, err)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("WaitContext did not return after context cancel")
	}

	// Ensure task can still complete
	dwg.Done()
	assert.Equal(t, 0, dwg.Count())
}
