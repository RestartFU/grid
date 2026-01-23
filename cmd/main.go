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
	"sync/atomic"

	"github.com/restartfu/grid/internal/specs"
	"github.com/restartfu/grid/internal/webhook"
	"github.com/restartfu/grid/internal/xmrig"
	"github.com/samber/lo"
)

func main() {
	webhookURL := flag.String("webhook", "", "discord webhook url (optional)")
	flag.Parse()

	startedAt := time.Now().UTC()
	cpuSpecs := lo.Must(specs.ReadSpecs())
	mgr, err := webhook.NewManager(*webhookURL, webhook.CPUSpecs{
		Model:      cpuSpecs.Model,
		Cores:      cpuSpecs.Cores,
		Threads:    cpuSpecs.Threads,
		Motherboard: cpuSpecs.Motherboard,
		CPUTemp:    cpuSpecs.CPUTemp,
		CPUWattage: cpuSpecs.CPUWattage,
		RAM:        cpuSpecs.RAM,
		RAMSpeed:   cpuSpecs.RAMSpeed,
	}, startedAt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "webhook init error: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigCh
		cancel()
	}()

	state := new(float64)
	running := &atomic.Bool{}
	go runXmrig(ctx, state, running)
	mgr.Start(ctx, state, running)
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

func runXmrig(ctx context.Context, hashrate *float64, running *atomic.Bool) {
	xmrigPath, err := exec.LookPath("xmrig")
	if err != nil {
		log.Fatalln(err)
	}

	args := []string{
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

	for {
		if ctx.Err() != nil {
			return
		}

		cmd := exec.CommandContext(ctx, xmrigPath, args...)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			log.Printf("xmrig stdout: %v", err)
			running.Store(false)
			sleepWithContext(ctx, time.Second*5)
			continue
		}
		if err := cmd.Start(); err != nil {
			log.Printf("xmrig start: %v", err)
			running.Store(false)
			sleepWithContext(ctx, time.Second*5)
			continue
		}
		running.Store(true)

		streamLogs(stdout, os.Stdout, hashrate)
		if err := cmd.Wait(); err != nil && ctx.Err() == nil {
			log.Printf("xmrig exited: %v", err)
		}
		running.Store(false)
		*hashrate = 0

		if !sleepWithContext(ctx, time.Second*5) {
			return
		}
	}
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
