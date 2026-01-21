package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/restartfu/grid/internal/cpu"
	"github.com/restartfu/grid/internal/webhook"
	"github.com/restartfu/grid/internal/xmrig"
	"github.com/samber/lo"
)

func main() {
	webhookURL := flag.String("webhook", "", "discord webhook url (optional)")
	flag.Parse()

	startedAt := time.Now().UTC()
	mgr, err := webhook.NewManager(*webhookURL, lo.Must(cpu.Model()), startedAt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "webhook init error: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	xmrigPath, err := exec.LookPath("xmrig")
	if err != nil {
		log.Fatalln(err)
	}
	cmd := exec.CommandContext(ctx, xmrigPath,
		"--url=tokyo:3333",
		"--user=%H",
		"--pass=%H",
		"--algo=rx/monero",
		"--cpu-priority=5",
		"--randomx-1gb-pages",
		"--huge-pages",
		"--no-color",
		"--print-time=5",
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatalln(err)
	}
	if err := cmd.Start(); err != nil {
		log.Fatalln(err)
	}

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigCh
		cancel()
		_ = cmd.Process.Signal(syscall.SIGTERM)
	}()

	state := new(float64)
	go streamLogs(stdout, os.Stdout, state)
	mgr.Start(ctx, state)
}

func streamLogs(r io.Reader, out io.Writer, hashrate *float64) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		_, _ = fmt.Fprintln(out, line)
		if value, ok := xmrig.ParseHashrateFromLog(line); ok {
			*hashrate = value
		}
	}
}
