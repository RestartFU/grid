package ports

import (
	"context"

	"github.com/restartfu/grid-node/internal/domain"
)

type SpecsReader interface {
	ReadSpecs(ctx context.Context) (domain.Specs, error)
}

type MetricsReader interface {
	ReadMetrics(ctx context.Context) (domain.Metrics, error)
}
