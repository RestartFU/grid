package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/restartfu/grid/internal/specs"
	"github.com/restartfu/rhookie"
	"github.com/samber/lo"
)

// Manager handles webhook updates and message persistence.
type Manager struct {
	hook             rhookie.Hook
	statePath        string
	baseTotalSeconds int64
	startedAt        time.Time
	title            string
	username         string
	specs            CPUSpecs

	mu    sync.Mutex
	state storedState
}

type storedState struct {
	MessageID           string  `json:"message_id"`
	BestHashrate        float64 `json:"best_hashrate"`
	LastHashrate        float64 `json:"last_hashrate"`
	TotalRuntimeSeconds int64   `json:"total_runtime_seconds"`
}

type CPUSpecs struct {
	Model       string
	Cores       int
	Threads     int
	Motherboard string
	CPUTemp     string
	CPUWattage  string
	RAM         string
	RAMSpeed    string
}

// NewManager creates a webhook manager backed by a JSON state file.
func NewManager(webhookURL string, specs CPUSpecs, startedAt time.Time) (*Manager, error) {
	statePath := filepath.Join(lo.Must(os.UserConfigDir()), "grid")

	id, token, err := parseWebhookURL(webhookURL)
	if err != nil {
		return nil, err
	}
	if err := ensureDir(statePath); err != nil {
		return nil, err
	}

	state := storedState{}
	if err := readState(statePath, &state); err != nil {
		return nil, err
	}

	h := rhookie.NewHook(id, token)
	name := stripCoreInfo(specs.Model)
	username := os.Getenv("USER")
	if username == "" {
		if current, err := user.Current(); err == nil {
			username = current.Username
		}
	}
	if username == "" {
		username = name
	}
	return &Manager{
		hook:             h,
		statePath:        statePath,
		baseTotalSeconds: state.TotalRuntimeSeconds,
		startedAt:        startedAt,
		title:            name,
		username:         username,
		specs:            specs,
		state:            state,
	}, nil
}

// Start begins periodic webhook updates and posts a down status on shutdown.
func (m *Manager) Start(ctx context.Context, hashrate *float64) {
	ticker := time.NewTicker(time.Second * 10)
	defer ticker.Stop()
	defer m.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		m.updateStats(*hashrate)
		m.refreshDynamicSpecs()
		fields := append(m.specFields(), m.bestHashrateField(), m.runtimeField(), m.powerCostField(), m.updatedField())
		payload := rhookie.Payload{}.
			WithEmbeds(rhookie.Embed{}.
				WithType("rich").
				WithTitle(m.title).
				WithDescription(formatHashrate(*hashrate)).
				WithFields(fields...).
				WithFooter(rhookie.Footer{Text: m.footerText()}).
				WithColor(5763719)).
			WithUsername(m.username)

		m.sendOrEditWithRetry(ctx, payload)
	}
}

// Stop posts a down status update once.
func (m *Manager) Stop() {
	m.refreshDynamicSpecs()
	fields := append(m.specFields(), m.bestHashrateField(), m.runtimeField(), m.powerCostField(), m.updatedField())
	payload := rhookie.Payload{}.
		WithEmbeds(rhookie.Embed{}.
			WithType("rich").
			WithTitle("Miner Down").
			WithDescription("miner is down").
			WithFields(fields...).
			WithFooter(rhookie.Footer{Text: m.footerText()}).
			WithColor(16711680)).
		WithUsername(m.username)

	m.sendOrEditWithRetry(context.Background(), payload)

	m.updateTotalRuntime()
}

func (m *Manager) getMessageID() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state.MessageID, nil
}

func (m *Manager) updateStats(value float64) {
	if err := m.updateState(func(s *storedState) {
		s.LastHashrate = value
		if s.BestHashrate == 0 || value > s.BestHashrate {
			s.BestHashrate = value
		}
		s.TotalRuntimeSeconds = m.baseTotalSeconds + int64(time.Since(m.startedAt).Seconds())
	}); err != nil {
		log.Printf("update state: %v", err)
	}
}

func (m *Manager) updateTotalRuntime() {
	if err := m.updateState(func(s *storedState) {
		s.TotalRuntimeSeconds = m.baseTotalSeconds + int64(time.Since(m.startedAt).Seconds())
	}); err != nil {
		log.Printf("update total runtime: %v", err)
	}
}

func (m *Manager) updateState(update func(*storedState)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	update(&m.state)
	return writeState(m.statePath, &m.state)
}

func (m *Manager) statsSnapshot() storedState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

func (m *Manager) refreshDynamicSpecs() {
	m.specs.CPUTemp = specs.ReadCPUTemp()
	m.specs.CPUWattage = specs.ReadCPUWattage()
}

func (m *Manager) sendOrEditWithRetry(ctx context.Context, payload rhookie.Payload) {
	for {
		if ctx.Err() != nil {
			return
		}
		messageID, err := m.getMessageID()
		if err != nil {
			log.Printf("load message id: %v", err)
			if !sleepWithContext(ctx, time.Second*5) {
				return
			}
			continue
		}

		if messageID == "" {
			msg, err := m.hook.SendMessageWithResponse(payload)
			if err != nil {
				log.Printf("send message: %v", err)
				if !sleepWithContext(ctx, time.Second*5) {
					return
				}
				continue
			}
			if err := m.updateState(func(s *storedState) {
				s.MessageID = msg.ID
			}); err != nil {
				log.Printf("save message id: %v", err)
			}
			return
		}

		if err := m.hook.EditMessage(messageID, payload); err != nil {
			log.Printf("edit message: %v", err)
			if !sleepWithContext(ctx, time.Second*5) {
				return
			}
			continue
		}
		return
	}
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

func (m *Manager) specFields() []rhookie.Field {
	var fields []rhookie.Field
	if m.specs.Cores > 0 && m.specs.Threads > 0 {
		fields = append(fields, rhookie.Field{}.
			WithName("Cores/Threads").
			WithValue(fmt.Sprintf("%dC / %dT", m.specs.Cores, m.specs.Threads)).
			WithInline(true))
	}
	if m.specs.CPUTemp != "" {
		fields = append(fields, rhookie.Field{}.
			WithName("CPU Temp").
			WithValue(m.specs.CPUTemp).
			WithInline(true))
	}
	if m.specs.CPUWattage != "" {
		fields = append(fields, rhookie.Field{}.
			WithName("CPU Power").
			WithValue(m.specs.CPUWattage).
			WithInline(true))
	}
	if m.specs.RAM != "" {
		fields = append(fields, rhookie.Field{}.
			WithName("RAM").
			WithValue(m.specs.RAM).
			WithInline(true))
	}
	if m.specs.RAMSpeed != "" {
		fields = append(fields, rhookie.Field{}.
			WithName("RAM Speed").
			WithValue(m.specs.RAMSpeed).
			WithInline(true))
	}
	if m.specs.Motherboard != "" {
		fields = append(fields, rhookie.Field{}.
			WithName("Motherboard").
			WithValue(m.specs.Motherboard).
			WithInline(true))
	}
	return fields
}

func (m *Manager) footerText() string {
	uptime := formatUptime(time.Since(m.startedAt))
	return fmt.Sprintf("Uptime: %s", uptime)
}

func (m *Manager) updatedField() rhookie.Field {
	return rhookie.Field{}.
		WithName("Updated").
		WithValue(fmt.Sprintf("<t:%d:R>", time.Now().Unix())).
		WithInline(true)
}

func (m *Manager) runtimeField() rhookie.Field {
	stats := m.statsSnapshot()
	return rhookie.Field{}.
		WithName("Total Runtime").
		WithValue(formatDurationSeconds(stats.TotalRuntimeSeconds)).
		WithInline(true)
}

func (m *Manager) powerCostField() rhookie.Field {
	stats := m.statsSnapshot()
	return rhookie.Field{}.
		WithName("Electricity Cost").
		WithValue(formatPowerCost(stats.TotalRuntimeSeconds)).
		WithInline(true)
}

func (m *Manager) bestHashrateField() rhookie.Field {
	stats := m.statsSnapshot()
	if stats.BestHashrate <= 0 {
		return rhookie.Field{}.
			WithName("Best Hashrate").
			WithValue("unknown").
			WithInline(true)
	}
	return rhookie.Field{}.
		WithName("Best Hashrate").
		WithValue(formatHashrate(stats.BestHashrate)).
		WithInline(true)
}

func parseWebhookURL(raw string) (string, string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", "", err
	}
	path := strings.Trim(parsed.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("expected .../webhooks/{id}/{token}")
	}
	return parts[len(parts)-2], parts[len(parts)-1], nil
}

func ensureDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "." || dir == "/" {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

func readState(path string, state *storedState) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return json.Unmarshal(data, state)
}

func writeState(path string, state *storedState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func formatUptime(d time.Duration) string {
	if d < time.Minute {
		return "less than a minute"
	}
	hours := d / time.Hour
	minutes := (d % time.Hour) / time.Minute
	if hours == 0 {
		return fmt.Sprintf("%d minutes", minutes)
	}
	if minutes == 0 {
		return fmt.Sprintf("%d hours", hours)
	}
	return fmt.Sprintf("%d hours %d minutes", hours, minutes)
}

func formatDurationSeconds(seconds int64) string {
	if seconds <= 0 {
		return "less than a minute"
	}
	return formatUptime(time.Duration(seconds) * time.Second)
}

func formatPowerCost(seconds int64) string {
	if seconds <= 0 {
		return "CAD 0.00"
	}
	const (
		watts       = 200.0
		ratePerKWh  = 0.10652
	)
	hours := float64(seconds) / 3600.0
	kwh := (watts / 1000.0) * hours
	cost := kwh * ratePerKWh
	return fmt.Sprintf("CAD %.2f", cost)
}

func formatHashrate(value float64) string {
	if value >= 1_000_000 {
		return fmt.Sprintf("%.2f MH/s", value/1_000_000)
	}
	if value >= 1_000 {
		return fmt.Sprintf("%.2f KH/s", value/1_000)
	}
	return fmt.Sprintf("%.2f H/s", value)
}

var coreInfoPattern = regexp.MustCompile(`(?i)\b\d+\s*-?\s*core(?:s)?(?:\s+processor)?\b`)

func stripCoreInfo(model string) string {
	cleaned := coreInfoPattern.ReplaceAllString(model, "")
	return strings.Join(strings.Fields(cleaned), " ")
}
