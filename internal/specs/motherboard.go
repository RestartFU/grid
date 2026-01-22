package specs

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func readMotherboard() string {
	vendor := readDMIFile("/sys/devices/virtual/dmi/id/board_vendor")
	name := readDMIFile("/sys/devices/virtual/dmi/id/board_name")
	if isUsefulDMIValue(vendor) || isUsefulDMIValue(name) {
		combined := strings.TrimSpace(strings.TrimSpace(vendor) + " " + strings.TrimSpace(name))
		return strings.Join(strings.Fields(combined), " ")
	}

	manufacturer, product := readBaseboardFromDMI()
	if manufacturer == "" && product == "" {
		return ""
	}
	combined := strings.TrimSpace(strings.TrimSpace(manufacturer) + " " + strings.TrimSpace(product))
	return strings.Join(strings.Fields(combined), " ")
}

func readDMIFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func isUsefulDMIValue(value string) bool {
	if value == "" {
		return false
	}
	lower := strings.ToLower(value)
	if lower == "unknown" || lower == "default string" || strings.Contains(lower, "to be filled") {
		return false
	}
	return true
}

func readBaseboardFromDMI() (string, string) {
	out, err := runDMIDecodeBaseboard()
	if err != nil {
		return "", ""
	}

	var manufacturer string
	var product string
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "Manufacturer:") {
			manufacturer = strings.TrimSpace(strings.TrimPrefix(line, "Manufacturer:"))
		} else if strings.HasPrefix(line, "Product Name:") {
			product = strings.TrimSpace(strings.TrimPrefix(line, "Product Name:"))
		}
	}
	return manufacturer, product
}

func runDMIDecodeBaseboard() ([]byte, error) {
	commands := [][]string{
		{"dmidecode", "-t", "baseboard"},
		{"/usr/bin/dmidecode", "-t", "baseboard"},
		{"/usr/sbin/dmidecode", "-t", "baseboard"},
		{"/sbin/dmidecode", "-t", "baseboard"},
	}

	for _, args := range commands {
		out, err := exec.Command(args[0], args[1:]...).Output()
		if err == nil {
			return out, nil
		}
		if isUsableBaseboardOutput(out) {
			return out, nil
		}
	}

	for _, args := range commands {
		sudoArgs := append([]string{"-n", args[0]}, args[1:]...)
		out, err := exec.Command("sudo", sudoArgs...).Output()
		if err == nil {
			return out, nil
		}
		if isUsableBaseboardOutput(out) {
			return out, nil
		}
	}

	return nil, fmt.Errorf("dmidecode unavailable")
}

func isUsableBaseboardOutput(out []byte) bool {
	if len(out) == 0 {
		return false
	}
	text := string(out)
	return strings.Contains(text, "Base Board Information") || strings.Contains(text, "Baseboard")
}
