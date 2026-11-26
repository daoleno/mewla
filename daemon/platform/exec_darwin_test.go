package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ebitengine/purego"
)

// The exec after Go callbacks on dispatch-queue threads, as the daemon's
// ScreenCaptureKit and Accessibility code gets them (exec_darwin.go): plain
// syscall.Exec stalls there on this Mac's runtime; ExecReplace must not.

const execAfterCallbacksEnv = "MEWLA_EXEC_AFTER_CALLBACKS_DIR"

var execSink uint64

func TestExecAfterCallbacksHelper(t *testing.T) {
	dir := os.Getenv(execAfterCallbacksEnv)
	if dir == "" {
		t.Skip("helper process for TestExecReplaceAfterDispatchCallbacks")
	}
	if _, err := os.Stat(filepath.Join(dir, "first")); err == nil {
		_ = os.WriteFile(filepath.Join(dir, "second"), nil, 0o600)
		os.Exit(0)
	}
	_ = os.WriteFile(filepath.Join(dir, "first"), nil, 0o600)
	lib, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		t.Fatal(err)
	}
	var globalQueue func(identifier int64, flags uint64) uintptr
	var asyncF func(queue, ctx, fn uintptr)
	purego.RegisterLibFunc(&globalQueue, lib, "dispatch_get_global_queue")
	purego.RegisterLibFunc(&asyncF, lib, "dispatch_async_f")
	var wg sync.WaitGroup
	callback := purego.NewCallback(func(ctx uintptr) uintptr {
		x := uint64(ctx) | 1
		for i := 0; i < 200000; i++ {
			x = x*6364136223846793005 + 1442695040888963407
		}
		execSink += x
		wg.Done()
		return 0
	})
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				runtimeGC()
			}
		}
	}()
	queue := globalQueue(0, 0)
	for round := 0; round < 400; round++ {
		wg.Add(8)
		for i := 0; i < 8; i++ {
			asyncF(queue, uintptr(round*8+i), callback)
		}
		wg.Wait()
	}
	close(stop)
	executable, _ := os.Executable()
	err = ExecReplace(executable, os.Args, os.Environ())
	_ = os.WriteFile(filepath.Join(dir, "exec-error"), []byte(err.Error()), 0o600)
	os.Exit(3)
}

func TestExecReplaceAfterDispatchCallbacks(t *testing.T) {
	if os.Getenv(execAfterCallbacksEnv) != "" {
		t.Skip("in the helper")
	}
	for run := 0; run < 3; run++ {
		dir := t.TempDir()
		cmd := exec.Command(os.Args[0], "-test.run=^TestExecAfterCallbacksHelper$")
		cmd.Env = append(os.Environ(), execAfterCallbacksEnv+"="+dir)
		var output strings.Builder
		cmd.Stdout, cmd.Stderr = &output, &output
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("run %d: %v\n%s", run, err, output.String())
			}
		case <-time.After(60 * time.Second):
			_ = cmd.Process.Kill()
			t.Fatalf("run %d: the exec after dispatch callbacks did not finish in 60s\n%s", run, output.String())
		}
		if _, err := os.Stat(filepath.Join(dir, "second")); err != nil {
			t.Fatalf("run %d: the new image did not run\n%s", run, output.String())
		}
	}
}

func runtimeGC() { runtime.GC() }
