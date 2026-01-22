package specs

import (
	"runtime"
	"testing"
)

func TestSpecs(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("cpu specs require /proc/cpuinfo on linux")
	}

	specs, err := ReadSpecs()
	if err != nil {
		t.Fatalf("Specs() error: %v", err)
	}

	t.Logf("Model: %s", specs.Model)
	t.Logf("Cores: %d", specs.Cores)
	t.Logf("Threads: %d", specs.Threads)
	if specs.Motherboard != "" {
		t.Logf("Motherboard: %s", specs.Motherboard)
	}
	if specs.RAM != "" {
		t.Logf("RAM: %s", specs.RAM)
	}
	if specs.RAMSpeed != "" {
		t.Logf("RAM Speed: %s", specs.RAMSpeed)
	}
}
