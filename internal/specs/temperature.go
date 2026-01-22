package specs

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

func readCPUTemp() string {
	if temp := readSensorsTemp(); temp != "" {
		return temp
	}
	return readSysfsTemp()
}

func readSysfsTemp() string {
	zones, _ := filepath.Glob("/sys/class/thermal/thermal_zone*")
	if len(zones) == 0 {
		return ""
	}

	var maxTemp float64
	var found bool
	for _, zone := range zones {
		tempPath := filepath.Join(zone, "temp")
		value := strings.TrimSpace(readSysfsFile(tempPath))
		if value == "" {
			continue
		}
		milli, err := strconv.ParseFloat(value, 64)
		if err != nil {
			continue
		}
		celsius := normalizeTemp(milli)
		if !found || celsius > maxTemp {
			maxTemp = celsius
			found = true
		}
	}

	if !found {
		return ""
	}
	return fmt.Sprintf("%.1f C", maxTemp)
}

func readSensorsTemp() string {
	if _, err := exec.LookPath("sensors"); err != nil {
		return ""
	}
	out, err := exec.Command("sensors").Output()
	if err != nil {
		return ""
	}

	temp, ok := parseSensorsOutput(out)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%.1f C", temp)
}

func parseSensorsOutput(out []byte) (float64, bool) {
	scanner := bufio.NewScanner(bytes.NewReader(out))
	tempPattern := regexp.MustCompile(`[-+]?\d+(?:\.\d+)?`)
	best := 0.0
	found := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if !strings.Contains(lower, "c") {
			continue
		}
		if !strings.Contains(line, "°") && !strings.Contains(lower, "c") {
			continue
		}

		isCPU := strings.Contains(lower, "temp") ||
			strings.Contains(lower, "tctl") ||
			strings.Contains(lower, "package") ||
			strings.Contains(lower, "cpu") ||
			strings.Contains(lower, "core")
		if !isCPU {
			continue
		}

		match := tempPattern.FindString(line)
		if match == "" {
			continue
		}
		value, err := strconv.ParseFloat(match, 64)
		if err != nil {
			continue
		}
		value = normalizeTemp(value)
		if !found || value > best {
			best = value
			found = true
		}
	}

	if found {
		return best, true
	}

	scanner = bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if !strings.Contains(lower, "c") {
			continue
		}
		match := tempPattern.FindString(line)
		if match == "" {
			continue
		}
		value, err := strconv.ParseFloat(match, 64)
		if err != nil {
			continue
		}
		value = normalizeTemp(value)
		if !found || value > best {
			best = value
			found = true
		}
	}

	return best, found
}

func normalizeTemp(value float64) float64 {
	if value > 1000 {
		value /= 1000
	}
	if value > 200 {
		value /= 1000
	}
	return value
}

func readSysfsFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}
