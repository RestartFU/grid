package specs

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

func readCPUWattage() string {
	if _, err := exec.LookPath("turbostat"); err != nil {
		return ""
	}

	baseArgs := []string{"--Summary", "--quiet", "--show", "PkgWatt", "-n", "1"}
	sudoArgs := append([]string{"-n", "turbostat"}, baseArgs...)
	out, err := exec.Command("sudo", sudoArgs...).Output()
	if err != nil {
		return ""
	}

	value := parseTurbostatPkgWatt(out)
	if value <= 0 {
		return ""
	}
	return fmt.Sprintf("%.1f W", value)
}

func ReadCPUWattage() string {
	return readCPUWattage()
}

func parseTurbostatPkgWatt(out []byte) float64 {
	scanner := bufio.NewScanner(bytes.NewReader(out))
	floatPattern := regexp.MustCompile(`[-+]?\d+(?:\.\d+)?`)
	columnIndex := -1

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.Contains(line, "turbostat") || strings.Contains(line, "Kernel") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		if columnIndex == -1 && strings.Contains(line, "PkgWatt") {
			for i, field := range fields {
				if field == "PkgWatt" {
					columnIndex = i
					break
				}
			}
			continue
		}

		if columnIndex >= 0 && columnIndex < len(fields) {
			parsed, err := strconv.ParseFloat(fields[columnIndex], 64)
			if err == nil {
				return parsed
			}
		}
		if columnIndex == -1 && len(fields) == 1 {
			match := floatPattern.FindString(fields[0])
			if match == "" {
				continue
			}
			parsed, err := strconv.ParseFloat(match, 64)
			if err == nil {
				return parsed
			}
		}
	}

	return 0
}
