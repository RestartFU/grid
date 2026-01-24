package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	sentryecho "github.com/getsentry/sentry-go/echo"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	httpadapter "github.com/restartfu/grid-node/internal/adapters/http"
	specsadapter "github.com/restartfu/grid-node/internal/adapters/specs"
	"github.com/restartfu/grid-node/internal/adapters/xmrig"
	"github.com/restartfu/grid-node/internal/app"
	"github.com/restartfu/grid-node/internal/observability"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	xmrigArgsFlag := flag.String("xmrig-args", "", "xmrig args, space-separated; overrides defaults")
	xmrigRestartDelayFlag := flag.Duration("xmrig-restart-delay", 0, "xmrig restart delay")
	flag.Parse()

	specsReader := specsadapter.NewReader()
	logger := log.Default()
	flushSentry, sentryEnabled, sentryErr := observability.InitSentry()
	if sentryErr != nil {
		logger.Printf("sentry init: %v", sentryErr)
		os.Exit(1)
	}
	defer flushSentry()

	envArgs := strings.TrimSpace(os.Getenv("GRID_XMRIG_ARGS"))
	envDelay := strings.TrimSpace(os.Getenv("GRID_XMRIG_RESTART_DELAY"))

	argsValue := strings.TrimSpace(*xmrigArgsFlag)
	if argsValue == "" && envArgs != "" {
		argsValue = envArgs
	}
	var args []string
	if argsValue != "" {
		args = strings.Fields(argsValue)
	}

	restartDelay := *xmrigRestartDelayFlag
	if restartDelay == 0 && envDelay != "" {
		parsed, err := time.ParseDuration(envDelay)
		if err != nil {
			log.Printf("invalid GRID_XMRIG_RESTART_DELAY: %v", err)
			os.Exit(1)
		} else {
			restartDelay = parsed
		}
	}
	if _, err := exec.LookPath("xmrig"); err != nil {
		logger.Printf("xmrig lookup: %v", err)
		os.Exit(1)
	}
	xmrigWrapper := xmrig.NewWrapper(os.Stdout, xmrig.Config{
		Args:         args,
		RestartDelay: restartDelay,
	})

	service := app.NewService(specsReader, specsReader, xmrigWrapper)
	httpServer := httpadapter.NewServer(service, logger)
	echoServer := echo.New()
	echoServer.HideBanner = true
	echoServer.Use(middleware.RequestIDWithConfig(middleware.RequestIDConfig{
		TargetHeader: echo.HeaderXRequestID,
		RequestIDHandler: func(c echo.Context, id string) {
			c.Request().Header.Set(echo.HeaderXRequestID, id)
		},
	}))
	if sentryEnabled {
		echoServer.Use(sentryecho.New(sentryecho.Options{
			Repanic:         true,
			WaitForDelivery: false,
		}))
	}
	echoServer.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: `{"time":"${time_rfc3339}","request_id":"${header:X-Request-ID}","remote_ip":"${remote_ip}","host":"${host}","method":"${method}","uri":"${uri}","status":${status},"latency":"${latency_human}","bytes_in":${bytes_in},"bytes_out":${bytes_out},"user_agent":"${user_agent}","error":"${error}"}` + "\n",
	}))
	echoServer.Use(middleware.Recover())
	echoServer.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			err := next(c)
			if err != nil {
				observability.CaptureError(err, map[string]string{
					"component": "http",
					"route":     c.Path(),
				}, map[string]interface{}{
					"method": c.Request().Method,
					"uri":    c.Request().RequestURI,
				})
			}
			return err
		}
	})
	httpServer.Register(echoServer)

	server := &http.Server{
		Addr:              *addr,
		Handler:           echoServer,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	go xmrigWrapper.Start(ctx)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}()

	log.Printf("grid-node http server listening on %s", *addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("listen: %v", err)
		os.Exit(1)
	}
}
