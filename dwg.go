package dwg

import (
	"context"
	"sync"
)

// DynamicWaitGroup is a synchronization primitive that extends the capabilities
// of sync.WaitGroup, allowing dynamic addition and completion of tasks.
// It provides mechanisms to lock the group, wait for tasks with context cancellation,
// and perform a graceful shutdown.
type DynamicWaitGroup struct {
	mutex sync.Mutex
	cond  *sync.Cond

	taskCounter    int
	waitingCounter int
	lockCounter    int
	locked         bool
	blocked        bool
	closed         chan struct{}
}

// NewDynamicWaitGroup creates and returns a new instance of DynamicWaitGroup.
func NewDynamicWaitGroup() *DynamicWaitGroup {
	task := &DynamicWaitGroup{}
	task.cond = sync.NewCond(&task.mutex)
	task.closed = make(chan struct{})
	return task
}

// Count returns the current number of active tasks in the DynamicWaitGroup.
func (d *DynamicWaitGroup) Count() int {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	return d.taskCounter
}

// Closed checks if the DynamicWaitGroup has been closed.
// Returns true if closed, false otherwise.
func (d *DynamicWaitGroup) Closed() bool {
	select {
	case <-d.closed:
		return true
	default:
		return false
	}
}

// Add increments or decrements the task counter by delta.
// If delta is positive, it adds tasks; if negative, it removes tasks.
// It blocks if the group is locked or if there are goroutines waiting.
// Returns false if the group is closed.
// May cause a panic if adding delta would result in a negative task counter.
func (d *DynamicWaitGroup) Add(delta int) bool {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	if d.taskCounter+delta < 0 {
		panic("DynamicWaitGroup: Add(delta) would result in a negative task counter")
	}

	for d.waitingCounter > 0 || d.lockCounter > 0 {
		select {
		case <-d.closed:
			return false
		default:
			d.cond.Wait()
		}
	}

	if d.Closed() {
		return false
	}

	d.taskCounter += delta
	return true
}

// Done decrements the task counter, indicating that a task has completed.
// Panics if called when the task counter is already zero.
func (d *DynamicWaitGroup) Done() {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	if d.taskCounter == 0 {
		panic("DynamicWaitGroup: Done called but the task counter is already zero")
	}

	d.taskCounter--
	if d.taskCounter == 0 && d.blocked {
		d.cond.Broadcast()
	}
}

// Wait blocks until all tasks have completed.
// It also blocks the addition of new tasks until completion.
func (d *DynamicWaitGroup) Wait() {
	d.wait(context.Background(), true, false)
}

// WaitContext blocks until all tasks have completed or the context is canceled.
// Returns an error if the context is canceled.
func (d *DynamicWaitGroup) WaitContext(ctx context.Context) error {
	return d.waitContext(ctx, true, false)
}

// Lock prevents new tasks from being added and waits for current tasks to finish.
// Use Unlock to release the lock.
func (d *DynamicWaitGroup) Lock() {
	d.wait(context.Background(), false, true)
}

// LockContext locks the group with context cancellation support.
// Returns an error if the context is canceled before locking is complete.
func (d *DynamicWaitGroup) LockContext(ctx context.Context) error {
	return d.waitContext(ctx, false, true)
}

// Unlock releases the lock acquired by Lock, allowing new tasks to be added.
// Panics if called without a matching Lock.
func (d *DynamicWaitGroup) Unlock() {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	select {
	case <-d.closed:
		return
	default:
	}

	if d.lockCounter == 0 {
		panic("DynamicWaitGroup: Unlock called without a matching Lock")
	}

	if d.waitingCounter == 0 {
		d.blocked = false
	}

	d.addLock(-1)
	if d.lockCounter == 0 {
		d.locked = false
	}

	d.cond.Broadcast()
}

// Close closes the DynamicWaitGroup, preventing further operations.
// Safe to call multiple times; subsequent calls have no effect.
func (d *DynamicWaitGroup) Close() {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	if d.Closed() {
		return
	}

	close(d.closed)
	d.cond.Broadcast()

	d.taskCounter = 0
	d.lockCounter = 0
	d.waitingCounter = 0
	d.blocked = false
	d.locked = false
	d.cond = nil
}

// Shutdown locks and closes the group, ensuring no new tasks can be added
// and that the group is closed after current tasks finish.
func (d *DynamicWaitGroup) Shutdown() {
	d.Lock()
	d.Close()
}

// addWaiting adjusts the waiting counter by the given value.
// It should be called with the mutex locked.
func (d *DynamicWaitGroup) addWaiting(value int) {
	if d.waitingCounter+value < 0 {
		return
	}
	d.waitingCounter += value
}

// addLock adjusts the lock counter by the given value.
// It should be called with the mutex locked.
func (d *DynamicWaitGroup) addLock(value int) {
	if d.lockCounter+value < 0 {
		return
	}
	d.lockCounter += value
}

// wait is an internal method that handles the waiting logic.
// It can be configured to unblock additions and/or acquire locks.
func (d *DynamicWaitGroup) wait(
	ctx context.Context,
	shouldUnblock bool,
	mustLock bool,
) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	var canceled bool

	d.addWaiting(1)
	defer d.addWaiting(-1)

	d.blocked = true
	if mustLock {
		d.addLock(1)
		d.locked = true
	}

	var err error
	for d.taskCounter > 0 && !canceled {
		select {
		case <-ctx.Done():
			err = ctx.Err()
			canceled = true
		case <-d.closed:
			return nil
		default:
			d.cond.Wait()
		}
	}

	if mustLock && canceled {
		d.addLock(-1)
		if d.lockCounter == 0 {
			d.locked = false
		}
	}

	if shouldUnblock && d.waitingCounter == 0 && !d.locked {
		d.blocked = false
	}

	d.cond.Broadcast()
	return err
}

// waitContext handles waiting with context cancellation.
// It starts a goroutine to manage the wait and returns when done or canceled.
func (d *DynamicWaitGroup) waitContext(
	ctx context.Context,
	shouldUnblock bool,
	mustLock bool,
) error {
	done := make(chan struct{})
	go func() {
		d.wait(ctx, shouldUnblock, mustLock)
		close(done)
	}()

	select {
	case <-ctx.Done():
		d.cond.Broadcast()
		return ctx.Err()
	case <-d.closed:
		return nil
	case <-done:
		return nil
	}
}
