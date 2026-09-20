package scenariolab

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"sync/atomic"
)

// OverlaySimBatchItem represents a scenario item sent to the headless simulation worker.
type OverlaySimBatchItem struct {
	Scenario      *Scenario      `json:"scenario"`
	ServerImpacts []ImpactRecord `json:"serverImpacts"`
	ClientFPS     int            `json:"clientFps"`
	ClientDtScale float64        `json:"clientDtScale"`
}

type workerResponse struct {
	Results []SimulationResult `json:"results"`
	Error   string             `json:"error,omitempty"`
	Status  string             `json:"status,omitempty"`
}

// NodeWorker represents a single persistent long-running Node.js process.
type NodeWorker struct {
	id     int
	script string
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *bufio.Reader
	mu     sync.Mutex
}

func startWorker(id int, scriptPath string) (*NodeWorker, error) {
	cmd := exec.Command("node", scriptPath)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to open stdin pipe for worker %d: %w", id, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("failed to open stdout pipe for worker %d: %w", id, err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("failed to start worker %d process: %w", id, err)
	}

	reader := bufio.NewReaderSize(stdout, 1024*1024)
	w := &NodeWorker{
		id:     id,
		script: scriptPath,
		cmd:    cmd,
		stdin:  stdin,
		reader: reader,
	}

	if err := w.Reset(); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("worker %d failed readiness check: %w", id, err)
	}

	return w, nil
}

// Reset pings the worker process with a RESET command to verify readiness and clear any state.
func (w *NodeWorker) Reset() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, err := fmt.Fprintln(w.stdin, "RESET"); err != nil {
		return err
	}
	line, err := w.reader.ReadBytes('\n')
	if err != nil {
		return err
	}

	var resp workerResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return fmt.Errorf("invalid reset response: %w (line: %q)", err, string(line))
	}
	if resp.Error != "" {
		return fmt.Errorf("reset returned error: %s", resp.Error)
	}
	return nil
}

// RunBatch sends a batch of simulation items to the worker and parses the simulation results.
func (w *NodeWorker) RunBatch(items []OverlaySimBatchItem) ([]SimulationResult, error) {
	if len(items) == 0 {
		return nil, nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	data, err := json.Marshal(items)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal batch items: %w", err)
	}

	if _, err := w.stdin.Write(data); err != nil {
		return nil, fmt.Errorf("failed to write batch to worker %d: %w", w.id, err)
	}
	if _, err := w.stdin.Write([]byte("\n")); err != nil {
		return nil, fmt.Errorf("failed to write newline to worker %d: %w", w.id, err)
	}

	line, err := w.reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read response from worker %d: %w", w.id, err)
	}

	var resp workerResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("worker %d returned malformed JSON: %w (line: %q)", w.id, err, string(line))
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("worker %d simulation failed: %s", w.id, resp.Error)
	}
	if len(resp.Results) != len(items) {
		return nil, fmt.Errorf("worker %d returned %d results, expected %d", w.id, len(resp.Results), len(items))
	}

	return resp.Results, nil
}

// Close gracefully closes stdin and waits for the worker process to exit.
func (w *NodeWorker) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.stdin != nil {
		_ = w.stdin.Close()
	}
	if w.cmd != nil && w.cmd.Process != nil {
		_ = w.cmd.Process.Kill()
		return w.cmd.Wait()
	}
	return nil
}

// NodeWorkerPool manages a pool of long-running, warm Node.js simulation workers.
type NodeWorkerPool struct {
	script    string
	size      int
	workers   []*NodeWorker
	available chan *NodeWorker
	closed    atomic.Bool
	mu        sync.Mutex
}

// NewNodeWorkerPool creates and warms up a pool of persistent Node.js workers.
func NewNodeWorkerPool(size int, scriptPath string) (*NodeWorkerPool, error) {
	if size <= 0 {
		size = runtime.GOMAXPROCS(0)
	}
	if size <= 0 {
		size = 1
	}

	pool := &NodeWorkerPool{
		script:    scriptPath,
		size:      size,
		workers:   make([]*NodeWorker, 0, size),
		available: make(chan *NodeWorker, size),
	}

	for i := 0; i < size; i++ {
		w, err := startWorker(i+1, scriptPath)
		if err != nil {
			pool.Close()
			return nil, fmt.Errorf("failed to initialize node worker pool (worker %d/%d): %w", i+1, size, err)
		}
		pool.workers = append(pool.workers, w)
		pool.available <- w
	}

	return pool, nil
}

// Size returns the number of active workers in the pool.
func (p *NodeWorkerPool) Size() int {
	return p.size
}

// RunBatch executes a single batch of items on the next available worker in the pool.
func (p *NodeWorkerPool) RunBatch(items []OverlaySimBatchItem) ([]SimulationResult, error) {
	if len(items) == 0 {
		return nil, nil
	}
	if p.closed.Load() {
		return nil, fmt.Errorf("worker pool is closed")
	}

	w, ok := <-p.available
	if !ok {
		return nil, fmt.Errorf("worker pool channel closed")
	}

	results, err := w.RunBatch(items)
	if err != nil {
		// Self-heal: attempt to replace dead worker
		newWorker, healErr := startWorker(w.id, p.script)
		if healErr == nil {
			_ = w.Close()
			w = newWorker
		}
	}

	p.available <- w
	return results, err
}

// RunBatchParallel distributes items across all workers in the pool concurrently.
func (p *NodeWorkerPool) RunBatchParallel(items []OverlaySimBatchItem) ([]SimulationResult, error) {
	if len(items) == 0 {
		return nil, nil
	}
	if p.size <= 1 || len(items) == 1 {
		return p.RunBatch(items)
	}

	results := make([]SimulationResult, len(items))
	numChunks := min(p.size, len(items))
	chunkSize := (len(items) + numChunks - 1) / numChunks

	var wg sync.WaitGroup
	var errMu sync.Mutex
	var firstErr error

	for c := range numChunks {
		start := c * chunkSize
		if start >= len(items) {
			break
		}
		end := min(start+chunkSize, len(items))

		wg.Add(1)
		go func(start, end int) {
			defer wg.Done()
			subRes, err := p.RunBatch(items[start:end])
			if err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
				return
			}
			copy(results[start:end], subRes)
		}(start, end)
	}

	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return results, nil
}

// ResetAll pings all workers with a RESET signal to verify health and clear state.
func (p *NodeWorkerPool) ResetAll() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, w := range p.workers {
		if err := w.Reset(); err != nil {
			return fmt.Errorf("worker %d reset failed: %w", w.id, err)
		}
	}
	return nil
}

// Close terminates all worker processes and releases resources.
func (p *NodeWorkerPool) Close() {
	if !p.closed.CompareAndSwap(false, true) {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	for _, w := range p.workers {
		_ = w.Close()
	}
	close(p.available)
}
