package domain

import "time"

type Health struct {
	Status string
	Time   time.Time
}

type Specs struct {
	Model       string
	Cores       int
	Threads     int
	Motherboard string
	CPUTemp     string
	CPUWattage  string
	RAM         string
	RAMSpeed    string
}

type Metrics struct {
	CPUTemp    string
	CPUWattage string
	Time       time.Time
}

type XMRigStatus struct {
	Running       bool
	HashrateHS    float64
	LastLogTime   *time.Time
	LastStartTime *time.Time
	LastExitTime  *time.Time
	LastError     string
}

type XMRigLogEntry struct {
	Time time.Time
	Line string
}
