package specs

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func readCPUInfo() (string, int, error) {
	file, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return "", 0, fmt.Errorf("failed to open /proc/cpuinfo: %w", err)
	}
	defer file.Close()

	info, err := readFirstCPUInfoBlock(file)
	if err != nil {
		return "", 0, err
	}

	model := info["model name"]
	if model == "" {
		return "", 0, fmt.Errorf("CPU model name not found in /proc/cpuinfo")
	}

	cores := parseInt(info["cpu cores"])
	return model, cores, nil
}

func readFirstCPUInfoBlock(file *os.File) (map[string]string, error) {
	info := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			break
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if key != "" {
			info[key] = value
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading /proc/cpuinfo: %w", err)
	}

	return info, nil
}

func parseInt(value string) int {
	if value == "" {
		return 0
	}
	parsed, err := strconv.Atoi(strings.Fields(value)[0])
	if err != nil {
		return 0
	}
	return parsed
}
