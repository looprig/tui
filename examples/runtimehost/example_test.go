package runtimehost_test

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/looprig/tui"
	"github.com/looprig/tui/runtime"
)

func Example_runtimeHost() {
	// Products own this constructor. It is where an already-composed Rig session
	// is wrapped with sessionadapter.New or sessionadapter.Restore.
	openAgent := func(context.Context) (tui.Agent, error) {
		return nil, errors.New("session unavailable")
	}

	// Isolate the runtime log created during this deterministic failure-path run.
	tempHome, err := os.MkdirTemp("", "looprig-tui-example-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tempHome)
	oldHome, hadHome := os.LookupEnv("HOME")
	if err := os.Setenv("HOME", tempHome); err != nil {
		panic(err)
	}
	defer func() {
		if hadHome {
			_ = os.Setenv("HOME", oldHome)
		} else {
			_ = os.Unsetenv("HOME")
		}
	}()

	exitCode := runtime.Run(
		context.Background(),
		openAgent,
		runtime.Banner{Name: "Purpose-built assistant", Description: "Terminal interface"},
	)
	fmt.Println("constructor failure exit code:", exitCode)

	// Output:
	// constructor failure exit code: 1
}
