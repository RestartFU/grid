package observability

import (
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/getsentry/sentry-go"
)

var sentryEnabled atomic.Bool

func InitSentry() (func(), bool, error) {
	dsn := strings.TrimSpace(os.Getenv("SENTRY_DSN"))
	if dsn == "" {
		sentryEnabled.Store(false)
		return func() {}, false, nil
	}

	options := sentry.ClientOptions{
		Dsn:              dsn,
		Environment:      strings.TrimSpace(os.Getenv("SENTRY_ENVIRONMENT")),
		Release:          strings.TrimSpace(os.Getenv("SENTRY_RELEASE")),
		AttachStacktrace: true,
	}

	if err := sentry.Init(options); err != nil {
		sentryEnabled.Store(false)
		return func() {}, false, err
	}

	sentryEnabled.Store(true)
	return func() {
		sentry.Flush(2 * time.Second)
	}, true, nil
}

func CaptureError(err error, tags map[string]string, extra map[string]interface{}) {
	if err == nil || !sentryEnabled.Load() {
		return
	}
	sentry.WithScope(func(scope *sentry.Scope) {
		for key, value := range tags {
			scope.SetTag(key, value)
		}
		for key, value := range extra {
			scope.SetExtra(key, value)
		}
		sentry.CaptureException(err)
	})
}

func Enabled() bool {
	return sentryEnabled.Load()
}
