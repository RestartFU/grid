package cpu

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type Specs struct {
	Model       string
	Cores       int
	Threads     int
	RAM         string
	RAMSpeed    string
}

func Model() (string, error) {
	specs, err := ReadSpecs()
	if err != nil {
		return "", err
	}
	return specs.Model, nil
}

func ReadSpecs() (Specs, error) {
	file, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return Specs{}, fmt.Errorf("failed to open /proc/cpuinfo: %w", err)
	}
	defer file.Close()

	info, err := readFirstCPUInfoBlock(file)
	if err != nil {
		return Specs{}, err
	}

	model := info["model name"]
	if model == "" {
		return Specs{}, fmt.Errorf("CPU model name not found in /proc/cpuinfo")
	}

	cores := parseInt(info["cpu cores"])
	ram := readRAM()
	ramSpeed := readRAMSpeed()

	return Specs{
		Model:      model,
		Cores:      cores,
		Threads:    runtime.NumCPU(),
		RAM:        ram,
		RAMSpeed:   ramSpeed,
	}, nil
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

func readRAM() string {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				kb, err := strconv.ParseFloat(parts[1], 64)
				if err != nil {
					return ""
				}
				return formatMemKB(kb)
			}
		}
	}
	return ""
}

func readRAMSpeed() string {
	values := readSysfsValues([]string{
		"/sys/devices/system/edac/mc/mc*/dimm*/dimm_speed",
	})
	if len(values) == 0 {
		values = readDMIMemoryValues()
	}
	if len(values) == 0 {
		return "unknown"
	}
	for i, value := range values {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			values[i] = fmt.Sprintf("%d MHz", parsed)
		}
	}
	return strings.Join(values, ", ")
}

func readSysfsValues(patterns []string) []string {
	seen := make(map[string]struct{})
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		for _, match := range matches {
			data, err := os.ReadFile(match)
			if err != nil {
				continue
			}
		value := strings.TrimSpace(string(data))
		if value == "" || strings.EqualFold(value, "unknown") {
			continue
		}
		seen[value] = struct{}{}
	}
	}
	if len(seen) == 0 {
		return nil
	}
	values := make([]string, 0, len(seen))
	for value := range seen {
		values = append(values, value)
	}
	return values
}

func readDMIMemoryValues() []string {
	speed, _ := readDMIMemoryInfo()
	return speed
}

func readDMIMemoryInfo() ([]string, []string) {
	out, err := runDMIDecode()
	if err != nil {
		return nil, nil
	}

	speedSet := make(map[string]struct{})
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if speed := parseDMISpeed(line); speed != "" {
			speedSet[speed] = struct{}{}
		}
	}

	return setToSlice(speedSet), nil
}

func runDMIDecode() ([]byte, error) {
	commands := [][]string{
		{"dmidecode", "-t", "memory"},
		{"/usr/bin/dmidecode", "-t", "memory"},
		{"/usr/sbin/dmidecode", "-t", "memory"},
		{"/sbin/dmidecode", "-t", "memory"},
	}

	for _, args := range commands {
		out, err := exec.Command(args[0], args[1:]...).Output()
		if err == nil {
			return out, nil
		}
		if isUsableDMIMemoryOutput(out) {
			return out, nil
		}
	}

	for _, args := range commands {
		sudoArgs := append([]string{"-n", args[0]}, args[1:]...)
		out, err := exec.Command("sudo", sudoArgs...).Output()
		if err == nil {
			return out, nil
		}
		if isUsableDMIMemoryOutput(out) {
			return out, nil
		}
	}

	return nil, fmt.Errorf("dmidecode unavailable")
}

func isUsableDMIMemoryOutput(out []byte) bool {
	if len(out) == 0 {
		return false
	}
	text := string(out)
	return strings.Contains(text, "Memory Device") || strings.Contains(text, "Physical Memory Array")
}

func parseDMISpeed(line string) string {
	prefixes := []string{
		"Configured Memory Speed:",
		"Speed:",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(line, prefix) {
			value := strings.TrimSpace(strings.TrimPrefix(line, prefix))
			if value == "" || strings.EqualFold(value, "unknown") || strings.Contains(value, "No Module") {
				return ""
			}
			return normalizeSpeedValue(value)
		}
	}
	return ""
}

func normalizeSpeedValue(value string) string {
	fields := strings.Fields(value)
	if len(fields) < 2 {
		return value
	}
	number := fields[0]
	unit := fields[1]
	if unit == "MT/s" || unit == "MHz" {
		return number + " MHz"
	}
	return value
}

func setToSlice(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	values := make([]string, 0, len(set))
	for value := range set {
		values = append(values, value)
	}
	return values
}

func formatMemKB(kb float64) string {
	const (
		kbPerMB = 1024
		kbPerGB = 1024 * 1024
	)
	if kb >= kbPerGB {
		return fmt.Sprintf("%.1f GB", kb/kbPerGB)
	}
	if kb >= kbPerMB {
		return fmt.Sprintf("%.0f MB", kb/kbPerMB)
	}
	return fmt.Sprintf("%.0f KB", kb)
}
