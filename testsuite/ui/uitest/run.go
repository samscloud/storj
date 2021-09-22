// Copyright (C) 2021 Storj Labs, Inc.
// See LICENSE for copying information.

package uitest

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/defaults"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
	"github.com/go-rod/rod/lib/utils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"storj.io/common/testcontext"
	"storj.io/storj/private/testplanet"
	"storj.io/storj/satellite"
)

// Our testing suite heavily uses randomly selected ports, which may collide
// with the launcher lock port. We'll disable the lock port entirely for
// the time being.
func init() { defaults.LockPort = 0 }

// Test defines common services for uitests.
type Test func(t *testing.T, ctx *testcontext.Context, planet *testplanet.Planet, browser *rod.Browser)

type zapWriter struct {
	*zap.Logger
}

func (log zapWriter) Write(data []byte) (int, error) {
	log.Logger.Info(string(data))
	return len(data), nil
}

// Run starts a new UI test.
func Run(t *testing.T, test Test) {
	testplanet.Run(t, testplanet.Config{
		SatelliteCount: 1, StorageNodeCount: 4, UplinkCount: 1,
		Reconfigure: testplanet.Reconfigure{
			Satellite: func(log *zap.Logger, index int, config *satellite.Config) {
				if dir := os.Getenv("STORJ_TEST_SATELLITE_WEB"); dir != "" {
					config.Console.StaticDir = dir
				}
				config.Console.NewOnboarding = true
			},
		},
		NonParallel: true,
	}, func(t *testing.T, ctx *testcontext.Context, planet *testplanet.Planet) {
		showBrowser := os.Getenv("STORJ_TEST_SHOW_BROWSER") != ""
		slowBrowser := os.Getenv("STORJ_TEST_SHOW_BROWSER") == "slow"

		logLauncher := zaptest.NewLogger(t).Named("launcher")

		browserLoaded := browserTimeoutDetector(10 * time.Second)
		defer browserLoaded()

		launch := launcher.New().
			Headless(!showBrowser).
			Leakless(false).
			Devtools(false).
			NoSandbox(true).
			UserDataDir(ctx.Dir("browser")).
			Logger(zapWriter{Logger: logLauncher}).
			Set("enable-logging").
			Set("disable-gpu")

		if browserHost := os.Getenv("STORJ_TEST_BROWER_HOSTPORT"); browserHost != "" {
			host, port, err := net.SplitHostPort(browserHost)
			require.NoError(t, err)
			launch = launch.Set("remote-debugging-address", host).Set(flags.RemoteDebuggingPort, port)
		}

		if browserBin := os.Getenv("STORJ_TEST_BROWSER"); browserBin != "" {
			launch = launch.Bin(browserBin)
		}

		defer func() {
			launch.Kill()
			avoidStall(3*time.Second, launch.Cleanup)
		}()

		url, err := launch.Launch()
		require.NoError(t, err)

		logBrowser := zaptest.NewLogger(t).Named("rod")

		browser := rod.New().
			Timeout(time.Minute).
			Sleeper(func() utils.Sleeper { return timeoutSleeper(5*time.Second, 5) }).
			ControlURL(url).
			Logger(utils.Log(func(msg ...interface{}) {
				logBrowser.Info(fmt.Sprintln(msg...))
			})).
			Context(ctx).
			WithPanic(func(v interface{}) { require.Fail(t, "check failed", v) })

		if slowBrowser {
			browser = browser.SlowMotion(300 * time.Millisecond).Trace(true)
		}

		defer ctx.Check(browser.Close)

		require.NoError(t, browser.Connect())

		browserLoaded()

		test(t, ctx, planet, browser)
	})
}

func browserTimeoutDetector(duration time.Duration) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		t := time.NewTimer(duration)
		defer t.Stop()
		select {
		case <-t.C:
			panic("timeout for starting browser exceeded")
		case <-ctx.Done():
			return
		}
	}()
	return cancel
}

func timeoutSleeper(totalSleep time.Duration, maxTries int) utils.Sleeper {
	singleSleep := totalSleep / time.Duration(maxTries)

	var slept int
	return func(ctx context.Context) error {
		slept++
		if slept > maxTries {
			return &utils.ErrMaxSleepCount{Max: maxTries}
		}

		t := time.NewTimer(singleSleep)
		defer t.Stop()
		select {
		case <-t.C:
		case <-ctx.Done():
		}

		return nil
	}
}

func avoidStall(maxDuration time.Duration, fn func()) {
	done := make(chan struct{})
	go func() {
		fn()
		close(done)
	}()

	timeout := time.NewTicker(maxDuration)
	defer timeout.Stop()
	select {
	case <-done:
	case <-timeout.C:
		fmt.Printf("go-rod did not shutdown within %v\n", maxDuration)
	}
}
