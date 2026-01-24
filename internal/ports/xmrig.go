package ports

import "github.com/restartfu/grid-node/internal/domain"

type XMRigMonitor interface {
	Status() domain.XMRigStatus
	Logs(n int) []domain.XMRigLogEntry
}
