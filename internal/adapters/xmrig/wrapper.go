package xmrig

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/restartfu/grid-node/internal/domain"
	"github.com/restartfu/grid-node/internal/observability"
)

type Wrapper struct {
	state  *state
	output io.Writer
	config Config
}

func NewWrapper(output io.Writer, config Config) *Wrapper {
	if output == nil {
		output = os.Stdout
	}
	config = normalizeConfig(config)
	return &Wrapper{
		state:  newState(),
		output: output,
		config: config,
	}
}

func (r *Wrapper) Start(ctx context.Context) {
	r.run(ctx)
}

func (r *Wrapper) Status() domain.XMRigStatus {
	return r.state.snapshot()
}

func (r *Wrapper) Logs(n int) []domain.XMRigLogEntry {
	return r.state.lastLogs(normalizeLogCount(n))
}

type state struct {
	mu        sync.RWMutex
	running   bool
	hashrate  float64
	lastLog   time.Time
	lastStart time.Time
	lastExit  time.Time
	lastError string
	logs      []domain.XMRigLogEntry
	logIndex  int
	logCount  int
}

func newState() *state {
	return &state{
		logs: make([]domain.XMRigLogEntry, maxLogs),
	}
}

func (s *state) snapshot() domain.XMRigStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	response := domain.XMRigStatus{
		Running:    s.running,
		HashrateHS: s.hashrate,
		LastError:  s.lastError,
	}
	if !s.lastLog.IsZero() {
		timestamp := s.lastLog
		response.LastLogTime = &timestamp
	}
	if !s.lastStart.IsZero() {
		timestamp := s.lastStart
		response.LastStartTime = &timestamp
	}
	if !s.lastExit.IsZero() {
		timestamp := s.lastExit
		response.LastExitTime = &timestamp
	}
	return response
}

func (s *state) recordStart(at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = true
	s.lastStart = at
	s.lastError = ""
}

func (s *state) recordExit(at time.Time, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.lastExit = at
	s.hashrate = 0
	if err != nil {
		s.lastError = err.Error()
	} else {
		s.lastError = ""
	}
}

func (s *state) recordLine(line string, at time.Time, hasHashrate bool, hashrate float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastLog = at
	s.logs[s.logIndex] = domain.XMRigLogEntry{
		Time: at,
		Line: line,
	}
	s.logIndex = (s.logIndex + 1) % maxLogs
	if s.logCount < maxLogs {
		s.logCount++
	}
	if hasHashrate {
		s.hashrate = hashrate
	}
}

func (s *state) lastLogs(count int) []domain.XMRigLogEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if count > s.logCount {
		count = s.logCount
	}
	if count == 0 {
		return []domain.XMRigLogEntry{}
	}
	logs := make([]domain.XMRigLogEntry, 0, count)
	if s.logCount < maxLogs {
		start := s.logCount - count
		for i := 0; i < count; i++ {
			logs = append(logs, s.logs[start+i])
		}
		return logs
	}
	start := s.logIndex - count
	if start < 0 {
		start += maxLogs
	}
	for i := 0; i < count; i++ {
		idx := (start + i) % maxLogs
		logs = append(logs, s.logs[idx])
	}
	return logs
}

func (r *Wrapper) streamLogs(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		if r.output != nil {
			_, _ = fmt.Fprintln(r.output, line)
		}
		now := time.Now().UTC()
		if value, ok := parseHashrateFromLog(line); ok {
			r.state.recordLine(line, now, true, value)
		} else {
			r.state.recordLine(line, now, false, 0)
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("xmrig log scan: %v", err)
		observability.CaptureError(err, map[string]string{
			"component": "xmrig",
			"operation": "log_scan",
		}, nil)
	}
}

func (r *Wrapper) run(ctx context.Context) {
	xmrigPath, err := exec.LookPath("xmrig")
	if err != nil {
		log.Printf("xmrig lookup: %v", err)
		r.state.recordExit(time.Now().UTC(), err)
		return
	}

	args := r.config.Args

	for {
		if ctx.Err() != nil {
			return
		}

		cmd := exec.CommandContext(ctx, xmrigPath, args...)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			log.Printf("xmrig stdout: %v", err)
			observability.CaptureError(err, map[string]string{
				"component": "xmrig",
				"operation": "stdout_pipe",
			}, nil)
			r.state.recordExit(time.Now().UTC(), err)
			if !sleepWithContext(ctx, r.config.RestartDelay) {
				return
			}
			continue
		}
		if err := cmd.Start(); err != nil {
			log.Printf("xmrig start: %v", err)
			observability.CaptureError(err, map[string]string{
				"component": "xmrig",
				"operation": "start",
			}, nil)
			r.state.recordExit(time.Now().UTC(), err)
			if !sleepWithContext(ctx, r.config.RestartDelay) {
				return
			}
			continue
		}
		r.state.recordStart(time.Now().UTC())

		r.streamLogs(stdout)
		waitErr := cmd.Wait()
		if waitErr != nil && ctx.Err() == nil {
			log.Printf("xmrig exited: %v", waitErr)
			observability.CaptureError(waitErr, map[string]string{
				"component": "xmrig",
				"operation": "wait",
			}, nil)
		}
		if ctx.Err() != nil {
			r.state.recordExit(time.Now().UTC(), nil)
			return
		}
		r.state.recordExit(time.Now().UTC(), waitErr)

		if !sleepWithContext(ctx, r.config.RestartDelay) {
			return
		}
	}
}

func normalizeLogCount(count int) int {
	if count <= 0 {
		return 0
	}
	if count > maxLogs {
		return maxLogs
	}
	return count
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
