package xmrig

import "time"

const maxLogs = 250

const defaultRestartDelay = 5 * time.Second

type Config struct {
	Args         []string
	RestartDelay time.Duration
}

var defaultArgs = []string{
	"--url=tokyo:3333",
	"--user=%H",
	"--pass=%H",
	"--algo=rx/monero",
	"--cpu-priority=5",
	"--randomx-1gb-pages",
	"--huge-pages",
	"--no-color",
	"--print-time=5",
}

func copyDefaultArgs() []string {
	args := make([]string, len(defaultArgs))
	copy(args, defaultArgs)
	return args
}

func normalizeConfig(cfg Config) Config {
	if cfg.RestartDelay <= 0 {
		cfg.RestartDelay = defaultRestartDelay
	}
	if len(cfg.Args) == 0 {
		cfg.Args = copyDefaultArgs()
	} else {
		args := make([]string, len(cfg.Args))
		copy(args, cfg.Args)
		cfg.Args = args
	}
	return cfg
}
