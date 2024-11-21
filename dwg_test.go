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

// TestNewChild verifica se um novo filho pode ser criado e adicionado corretamente ao pai.
func TestNewChild(t *testing.T) {
	parent := NewDynamicWaitGroup()
	if parent.HasChildren() {
		t.Fatal("O pai não deve ter filhos inicialmente")
	}

	child := parent.NewChild()
	if !parent.HasChildren() {
		t.Fatal("O pai deve ter um filho após NewChild()")
	}

	if child.parent != parent {
		t.Fatal("O pai do filho deve ser o DynamicWaitGroup pai")
	}

	if parent.TotalChildren() != 1 {
		t.Fatalf("Esperado 1 filho, mas obteve %d", parent.TotalChildren())
	}
}

// TestChildAddAndDone testa a adição de tarefas a um filho e garante que não afetem o pai.
func TestChildAddAndDone(t *testing.T) {
	parent := NewDynamicWaitGroup()
	child := parent.NewChild()

	child.Add(1)
	if child.Count() != 1 {
		t.Fatalf("Esperado contador de tarefas do filho igual a 1, mas obteve %d", child.Count())
	}

	if parent.Count() != 0 {
		t.Fatalf("Esperado contador de tarefas do pai igual a 0, mas obteve %d", parent.Count())
	}

	child.Done()
	if child.Count() != 0 {
		t.Fatalf("Esperado contador de tarefas do filho igual a 0 após Done(), mas obteve %d", child.Count())
	}
}

// TestWaitOnChild garante que o método Wait() em um filho funcione como esperado.
func TestWaitOnChild(t *testing.T) {
	child := NewDynamicWaitGroup()
	child.Add(1)

	done := make(chan struct{})
	go func() {
		child.Wait()
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("Wait() deve bloquear até que Done() seja chamado")
	case <-time.After(100 * time.Millisecond):
		// Comportamento esperado; Wait() deve bloquear
	}

	child.Done()

	select {
	case <-done:
		// Sucesso; Wait() desbloqueou após Done()
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Wait() deveria ter desbloqueado após Done() ser chamado")
	}
}

// TestParentWaitOnChildren garante que o pai possa esperar por seus filhos.
func TestParentWaitOnChildren(t *testing.T) {
	parent := NewDynamicWaitGroup()
	child1 := parent.NewChild()
	child2 := parent.NewChild()

	child1.Add(1)
	child2.Add(1)

	done := make(chan struct{})
	go func() {
		parent.Wait()
		close(done)
	}()

	// Permite que parent.Wait() comece
	time.Sleep(200 * time.Millisecond)

	select {
	case <-done:
		t.Fatal("Wait() do pai deve bloquear até que os filhos terminem")
	default:
		// Comportamento esperado
	}

	child1.Done()
	select {
	case <-done:
		t.Fatal("Wait() do pai ainda deve bloquear até que todos os filhos terminem")
	default:
		// Comportamento esperado
	}

	child2.Done()
	select {
	case <-done:
		// Sucesso; Wait() do pai deve desbloquear agora
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Wait() do pai deveria ter desbloqueado após todos os filhos terminarem")
	}
}

// TestCloseClosesChildren garante que fechar o pai fecha todos os seus filhos.
func TestCloseClosesChildren(t *testing.T) {
	parent := NewDynamicWaitGroup()
	child := parent.NewChild()

	parent.Close()

	if !child.Closed() {
		t.Fatal("O filho deve ser fechado quando o pai é fechado")
	}

	if parent.TotalChildren() != 0 {
		t.Fatalf("O pai não deve ter filhos após Close(), obteve %d", parent.TotalChildren())
	}
}

// TestShutdownShutsDownChildren garante que Shutdown() no pai desliga todos os filhos.
func TestShutdownShutsDownChildren(t *testing.T) {
	parent := NewDynamicWaitGroup()
	child := parent.NewChild()

	child.Add(1)

	done := make(chan struct{})
	go func() {
		parent.Shutdown()
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("Shutdown() deve bloquear até que as tarefas dos filhos terminem")
	case <-time.After(100 * time.Millisecond):
		// Comportamento esperado
	}

	// Agora marca a tarefa do filho como concluída
	child.Done()

	select {
	case <-done:
		// Sucesso; Shutdown() deve desbloquear agora
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Shutdown() deveria ter desbloqueado após as tarefas dos filhos terminarem")
	}

	if parent.Closed() && child.Closed() {
		// Sucesso; pai e filho devem estar fechados
	} else {
		t.Fatal("Pai e filho devem estar fechados após Shutdown()")
	}
}

// TestLockAndUnlockChildren garante que Lock() e Unlock() no pai afetem os filhos.
func TestLockAndUnlockChildren(t *testing.T) {
	parent := NewDynamicWaitGroup()
	child := parent.NewChild()

	child.Add(1)

	// Canal para sinalizar quando child.Add(1) for concluído
	done := make(chan struct{})

	// Bloqueia o pai
	go func() {
		parent.Lock()
	}()

	// Pequeno atraso para garantir que parent.Lock() tenha começado
	time.Sleep(100 * time.Millisecond)

	// Tenta adicionar uma tarefa ao filho; deve bloquear até que parent.Unlock() seja chamado
	added := make(chan bool)
	go func() {
		// child.Add(1) deve bloquear até que parent.Unlock() seja chamado
		success := child.Add(1)
		added <- success
	}()

	go func() {
		<-time.After(200 * time.Millisecond)
		child.Done()
	}()

	select {
	case <-added:
		t.Fatal("child.Add(1) não deveria ter completado enquanto o pai está bloqueado")
	case <-time.After(100 * time.Millisecond):
		// Comportamento esperado; child.Add(1) está bloqueado
	}

	// Desbloqueia o pai
	parent.Unlock()
	close(done)

	// Agora, child.Add(1) deve prosseguir
	select {
	case success := <-added:
		if !success {
			t.Fatal("child.Add(1) deveria ter retornado true após parent.Unlock()")
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("child.Add(1) deveria ter prosseguido após parent.Unlock()")
	}

	// Limpa as tarefas
	child.Done()

	// Verifica se o pai foi desbloqueado
	select {
	case <-done:
		// Sucesso; parent.Lock() foi completado
	default:
		t.Fatal("parent.Lock() deveria ter completado após todas as tarefas serem concluídas")
	}
}

// TestWaitContextWithChildren garante que WaitContext respeita o cancelamento do contexto.
func TestWaitContextWithChildren(t *testing.T) {
	parent := NewDynamicWaitGroup()
	child := parent.NewChild()

	child.Add(1)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := parent.WaitContext(ctx)
	if err == nil {
		t.Fatal("WaitContext deveria retornar erro quando o contexto é cancelado")
	}

	// Agora conclui a tarefa e tenta novamente
	child.Done()
	err = parent.WaitContext(context.Background())
	if err != nil {
		t.Fatalf("WaitContext não deveria retornar erro quando as tarefas estão concluídas, obteve %v", err)
	}
}
