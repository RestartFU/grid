package xmrig

import (
	"regexp"
	"strconv"
	"strings"
)

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// ParseHashrateFromLog parses a 10s hashrate line from xmrig output.
func ParseHashrateFromLog(line string) (float64, bool) {
	line = ansiRegex.ReplaceAllString(line, "")
	lower := strings.ToLower(line)
	if !strings.Contains(lower, "speed") {
		return 0, false
	}

	fields := strings.Fields(line)
	for i := 0; i+2 < len(fields); i++ {
		if !strings.EqualFold(fields[i], "speed") {
			continue
		}
		if !strings.EqualFold(fields[i+1], "10s/60s/15m") {
			continue
		}
		value, err := strconv.ParseFloat(fields[i+2], 64)
		if err != nil {
			return 0, false
		}
		unit := ""
		last := fields[len(fields)-1]
		if strings.HasSuffix(strings.ToLower(last), "h/s") {
			unit = last
		}
		return ScaleHashrate(value, unit), true
	}
	return 0, false
}

// ScaleHashrate converts the value into H/s.
func ScaleHashrate(value float64, unit string) float64 {
	switch strings.ToLower(unit) {
	case "kh/s":
		return value * 1e3
	case "mh/s":
		return value * 1e6
	case "gh/s":
		return value * 1e9
	case "th/s":
		return value * 1e12
	default:
		return value
	}
}
