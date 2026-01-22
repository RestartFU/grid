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
	Model      string
	Cores      int
	Threads    int
	RAM        string
	RAMSpeed   string
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

		fields := m.specFields()
		payload := rhookie.Payload{}.
			WithEmbeds(rhookie.Embed{}.
				WithType("rich").
				WithTitle(m.title).
				WithDescription(fmt.Sprintf("%.2f H/s", *hashrate)).
				WithFields(fields...).
				WithFooter(rhookie.Footer{Text: m.footerText()}).
				WithColor(5763719)).
			WithUsername(m.username)

		messageID, err := m.getMessageID()
		if err != nil {
			log.Printf("load message id: %v", err)
			continue
		}

		if messageID == "" {
			msg, err := m.hook.SendMessageWithResponse(payload)
			if err != nil {
				log.Printf("send message: %v", err)
				continue
			}
			if err := m.updateState(func(s *storedState) {
				s.MessageID = msg.ID
			}); err != nil {
				log.Printf("save message id: %v", err)
			}
			continue
		}

		if err := m.hook.EditMessage(messageID, payload); err != nil {
			log.Printf("edit message: %v", err)
			msg, sendErr := m.hook.SendMessageWithResponse(payload)
			if sendErr != nil {
				log.Printf("send message after edit failure: %v", sendErr)
				continue
			}
			if err := m.updateState(func(s *storedState) {
				s.MessageID = msg.ID
			}); err != nil {
				log.Printf("save message id after edit failure: %v", err)
			}
		}
	}
}

// Stop posts a down status update once.
func (m *Manager) Stop() {
	fields := m.specFields()
	payload := rhookie.Payload{}.
		WithEmbeds(rhookie.Embed{}.
			WithType("rich").
			WithTitle("Miner Down").
			WithDescription("miner is down").
			WithFields(fields...).
			WithFooter(rhookie.Footer{Text: m.footerText()}).
			WithColor(16711680)).
		WithUsername(m.username)

	messageID, err := m.getMessageID()
	if err != nil {
		log.Printf("load message id for shutdown: %v", err)
		return
	}
	if messageID == "" {
		msg, err := m.hook.SendMessageWithResponse(payload)
		if err != nil {
			log.Printf("send shutdown message: %v", err)
			return
		}
		if err := m.updateState(func(s *storedState) {
			s.MessageID = msg.ID
		}); err != nil {
			log.Printf("save shutdown message id: %v", err)
		}
		m.updateTotalRuntime()
		return
	}

	if err := m.hook.EditMessage(messageID, payload); err != nil {
		log.Printf("edit shutdown message: %v", err)
	}

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

func (m *Manager) specFields() []rhookie.Field {
	var fields []rhookie.Field
	if m.specs.Cores > 0 && m.specs.Threads > 0 {
		fields = append(fields, rhookie.Field{}.
			WithName("Cores/Threads").
			WithValue(fmt.Sprintf("%dC / %dT", m.specs.Cores, m.specs.Threads)))
	}
	if m.specs.RAM != "" {
		fields = append(fields, rhookie.Field{}.
			WithName("RAM").
			WithValue(m.specs.RAM))
	}
	if m.specs.RAMSpeed != "" {
		fields = append(fields, rhookie.Field{}.
			WithName("RAM Speed").
			WithValue(m.specs.RAMSpeed))
	}
	return fields
}

func (m *Manager) footerText() string {
	uptime := formatUptime(time.Since(m.startedAt))
	updated := time.Now().UTC().Format("2006-01-02 15:04:05 UTC")
	return fmt.Sprintf("Uptime: %s | Updated: %s", uptime, updated)
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

var coreInfoPattern = regexp.MustCompile(`(?i)\b\d+\s*-?\s*core(?:s)?(?:\s+processor)?\b`)

func stripCoreInfo(model string) string {
	cleaned := coreInfoPattern.ReplaceAllString(model, "")
	return strings.Join(strings.Fields(cleaned), " ")
}
