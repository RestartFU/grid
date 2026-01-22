package specs

import (
	"bufio"
	"bytes"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func readRAM() string {
	if total := readDMIMemoryCapacity(); total != "" {
		return total
	}

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

func readDMIMemoryCapacity() string {
	out, err := runDMIDecode()
	if err != nil {
		return ""
	}

	var totalKB float64
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "Size:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "Size:"))
		if value == "" || strings.EqualFold(value, "no module installed") {
			continue
		}
		amount, unit := parseDMIUnit(value)
		if amount == 0 || unit == "" {
			continue
		}
		switch unit {
		case "kb":
			totalKB += amount
		case "mb":
			totalKB += amount * 1024
		case "gb":
			totalKB += amount * 1024 * 1024
		}
	}

	if totalKB == 0 {
		return ""
	}
	return formatMemKB(totalKB)
}

func parseDMIUnit(value string) (float64, string) {
	fields := strings.Fields(value)
	if len(fields) < 2 {
		return 0, ""
	}
	amount, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, ""
	}
	unit := strings.ToLower(fields[1])
	return amount, unit
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
	if !strings.HasPrefix(line, "Speed:") {
		return ""
	}
	value := strings.TrimSpace(strings.TrimPrefix(line, "Speed:"))
	if value == "" || strings.EqualFold(value, "unknown") || strings.Contains(value, "No Module") {
		return ""
	}
	return normalizeSpeedValue(value)
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
		gb := kb / kbPerGB
		if gb >= 16 {
			return fmt.Sprintf("%.0f GB", math.Round(gb))
		}
		return fmt.Sprintf("%.1f GB", gb)
	}
	if kb >= kbPerMB {
		return fmt.Sprintf("%.0f MB", kb/kbPerMB)
	}
	return fmt.Sprintf("%.0f KB", kb)
}
