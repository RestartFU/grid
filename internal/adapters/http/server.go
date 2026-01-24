package http

import (
	"errors"
	"log"

	nethttp "net/http"

	"github.com/labstack/echo/v4"
	"github.com/restartfu/grid-node/internal/app"
	"github.com/restartfu/grid-node/internal/observability"
	"github.com/restartfu/grid-node/openapi/generated"
)

const maxXmrigLogs = 250

type Server struct {
	service *app.Service
	logger  *log.Logger
}

func NewServer(service *app.Service, logger *log.Logger) *Server {
	if logger == nil {
		logger = log.Default()
	}
	return &Server{
		service: service,
		logger:  logger,
	}
}

func (s *Server) Register(e *echo.Echo) {
	generated.RegisterHandlers(e, s)
}

func (s *Server) GetHealth(ctx echo.Context) error {
	health := s.service.Health()
	return ctx.JSON(nethttp.StatusOK, generated.Health{
		Status: health.Status,
		Time:   health.Time,
	})
}

func (s *Server) GetMetrics(ctx echo.Context) error {
	metrics, err := s.service.Metrics(ctx.Request().Context())
	if err != nil {
		observability.CaptureError(err, map[string]string{
			"component": "http",
			"handler":   "metrics",
		}, nil)
		return ctx.JSON(nethttp.StatusInternalServerError, generated.Error{Error: err.Error()})
	}
	return ctx.JSON(nethttp.StatusOK, generated.Metrics{
		CpuTemp:    metrics.CPUTemp,
		CpuWattage: metrics.CPUWattage,
		Time:       metrics.Time,
	})
}

func (s *Server) GetSpecs(ctx echo.Context) error {
	specs, err := s.service.Specs(ctx.Request().Context())
	if err != nil {
		observability.CaptureError(err, map[string]string{
			"component": "http",
			"handler":   "specs",
		}, nil)
		return ctx.JSON(nethttp.StatusInternalServerError, generated.Error{Error: err.Error()})
	}
	return ctx.JSON(nethttp.StatusOK, generated.Specs{
		Model:       specs.Model,
		Cores:       int32(specs.Cores),
		Threads:     int32(specs.Threads),
		Motherboard: specs.Motherboard,
		CpuTemp:     specs.CPUTemp,
		CpuWattage:  specs.CPUWattage,
		Ram:         specs.RAM,
		RamSpeed:    specs.RAMSpeed,
	})
}

func (s *Server) GetXmrigStatus(ctx echo.Context) error {
	status := s.service.XMRigStatus()
	response := generated.XMRigStatus{
		Running:    status.Running,
		HashrateHs: status.HashrateHS,
	}
	if status.LastError != "" {
		errCopy := status.LastError
		response.LastError = &errCopy
	}
	response.LastLogTime = status.LastLogTime
	response.LastStartTime = status.LastStartTime
	response.LastExitTime = status.LastExitTime
	return ctx.JSON(nethttp.StatusOK, response)
}

func (s *Server) GetXmrigLogs(ctx echo.Context, params generated.GetXmrigLogsParams) error {
	count, err := xmrigLogCount(params)
	if err != nil {
		return ctx.JSON(nethttp.StatusBadRequest, generated.Error{Error: "invalid n"})
	}
	logs := s.service.XMRigLogs(count)
	response := generated.XMRigLogs{
		Count: int32(len(logs)),
		Logs:  make([]generated.XMRigLogEntry, 0, len(logs)),
	}
	for _, entry := range logs {
		response.Logs = append(response.Logs, generated.XMRigLogEntry{
			Time: entry.Time,
			Line: entry.Line,
		})
	}
	return ctx.JSON(nethttp.StatusOK, response)
}

func xmrigLogCount(params generated.GetXmrigLogsParams) (int, error) {
	if params.N == nil {
		return maxXmrigLogs, nil
	}
	count := *params.N
	if count <= 0 {
		return 0, errInvalidLogCount
	}
	if count > maxXmrigLogs {
		return maxXmrigLogs, nil
	}
	return count, nil
}

var errInvalidLogCount = errors.New("invalid log count")
