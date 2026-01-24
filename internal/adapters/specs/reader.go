package specsadapter

import (
	"context"
	"time"

	"github.com/restartfu/grid-node/internal/domain"
	"github.com/restartfu/grid-node/internal/observability"
	"github.com/restartfu/grid-node/internal/specs"
)

type Reader struct{}

func NewReader() *Reader {
	return &Reader{}
}

func (r *Reader) ReadSpecs(ctx context.Context) (domain.Specs, error) {
	if err := ctx.Err(); err != nil {
		return domain.Specs{}, err
	}
	current, err := specs.ReadSpecs()
	if err != nil {
		observability.CaptureError(err, map[string]string{
			"component": "specs",
			"operation": "read_specs",
		}, nil)
		return domain.Specs{}, err
	}
	return domain.Specs{
		Model:       current.Model,
		Cores:       current.Cores,
		Threads:     current.Threads,
		Motherboard: current.Motherboard,
		CPUTemp:     current.CPUTemp,
		CPUWattage:  current.CPUWattage,
		RAM:         current.RAM,
		RAMSpeed:    current.RAMSpeed,
	}, nil
}

func (r *Reader) ReadMetrics(ctx context.Context) (domain.Metrics, error) {
	if err := ctx.Err(); err != nil {
		return domain.Metrics{}, err
	}
	return domain.Metrics{
		CPUTemp:    specs.ReadCPUTemp(),
		CPUWattage: specs.ReadCPUWattage(),
		Time:       time.Now().UTC(),
	}, nil
}
