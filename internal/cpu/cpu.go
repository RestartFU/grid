package cpu

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func Model() (string, error) {
	file, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return "", fmt.Errorf("failed to open /proc/cpuinfo: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "model name") {
			parts := strings.Split(line, ":")
			if len(parts) > 1 {
				return strings.TrimSpace(parts[1]), nil
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("error reading /proc/cpuinfo: %w", err)
	}

	return "", fmt.Errorf("CPU model name not found in /proc/cpuinfo")
}
