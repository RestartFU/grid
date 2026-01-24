package app

import (
	"context"
	"time"

	"github.com/restartfu/grid-node/internal/domain"
	"github.com/restartfu/grid-node/internal/ports"
)

type Service struct {
	specsReader   ports.SpecsReader
	metricsReader ports.MetricsReader
	xmrigMonitor  ports.XMRigMonitor
}

func NewService(specsReader ports.SpecsReader, metricsReader ports.MetricsReader, xmrigMonitor ports.XMRigMonitor) *Service {
	return &Service{
		specsReader:   specsReader,
		metricsReader: metricsReader,
		xmrigMonitor:  xmrigMonitor,
	}
}

func (s *Service) Health() domain.Health {
	return domain.Health{
		Status: "ok",
		Time:   time.Now().UTC(),
	}
}

func (s *Service) Specs(ctx context.Context) (domain.Specs, error) {
	return s.specsReader.ReadSpecs(ctx)
}

func (s *Service) Metrics(ctx context.Context) (domain.Metrics, error) {
	return s.metricsReader.ReadMetrics(ctx)
}

func (s *Service) XMRigStatus() domain.XMRigStatus {
	if s.xmrigMonitor == nil {
		return domain.XMRigStatus{}
	}
	return s.xmrigMonitor.Status()
}

func (s *Service) XMRigLogs(n int) []domain.XMRigLogEntry {
	if s.xmrigMonitor == nil {
		return []domain.XMRigLogEntry{}
	}
	return s.xmrigMonitor.Logs(n)
}
