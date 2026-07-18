package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const signalHelperEnv = "MUXLM_TEST_SIGNAL_HELPER"

func TestMuxLMSignalHelperProcess(t *testing.T) {
	mode := os.Getenv(signalHelperEnv)
	if mode == "" {
		return
	}

	capture := os.Getenv("SIGNAL_HELPER_CAPTURE")
	ready := os.Getenv("SIGNAL_HELPER_READY")
	first := os.Getenv("SIGNAL_HELPER_FIRST")
	if err := os.WriteFile(capture, []byte(os.Getenv("CODEX_HOME")), 0o600); err != nil {
		t.Fatal(err)
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	defer signal.Stop(signals)
	if err := os.WriteFile(ready, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	received := <-signals
	sig, ok := received.(syscall.Signal)
	if !ok {
		t.Fatalf("unexpected signal type %T", received)
	}
	if err := os.WriteFile(first, []byte(strconv.Itoa(int(sig))), 0o600); err != nil {
		t.Fatal(err)
	}
	if mode == "graceful" {
		return
	}
	select {}
}

func TestSignalExitCodeIsNormalized(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("MuxLM release targets are Unix platforms")
	}
	err := exec.Command("sh", "-c", "kill -TERM $$").Run()
	normalized := normalizeCommandError(err)
	exit, ok := normalized.(interface{ ExitCode() int })
	if !ok || exit.ExitCode() != 128+int(syscall.SIGTERM) {
		t.Fatalf("normalized signal error = %#v", normalized)
	}
	var original *exec.ExitError
	if !errors.As(normalized, &original) {
		t.Fatal("normalized error no longer unwraps to exec.ExitError")
	}
}

func TestLaunchCleansSecretDirectoryAfterTermSignal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("MuxLM release targets are Unix platforms")
	}
	capture, ready, first := installSignalHelper(t, "graceful")

	signalSent := make(chan error, 1)
	go func() {
		if err := waitForSignalFile(ready); err != nil {
			signalSent <- err
			return
		}
		signalSent <- syscall.Kill(os.Getpid(), syscall.SIGTERM)
	}()

	provider := Provider{ID: "signal-test", Name: "Signal Test", OpenAIURL: "https://example.com", Key: "secret"}
	if err := launchCodex(&provider, "model", false, false, signalHelperArgs()); err != nil {
		t.Fatalf("launch after forwarded signal: %v", err)
	}
	if err := <-signalSent; err != nil {
		t.Fatal(err)
	}
	assertForwardedSignal(t, first, syscall.SIGTERM)
	assertCapturedDirRemoved(t, capture)
}

func TestLaunchForcesIgnoredChildOnSecondSignalAndCleansSecretDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("MuxLM release targets are Unix platforms")
	}
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT} {
		t.Run(sig.String(), func(t *testing.T) {
			capture, ready, first := installSignalHelper(t, "ignore")
			signalSent := make(chan error, 1)
			go func() {
				if err := waitForSignalFile(ready); err != nil {
					signalSent <- err
					return
				}
				if err := syscall.Kill(os.Getpid(), sig); err != nil {
					signalSent <- err
					return
				}
				if err := waitForSignalFile(first); err != nil {
					// Unblock run even when forwarding regresses, so the test fails
					// promptly instead of leaving an ignored child behind.
					_ = syscall.Kill(os.Getpid(), sig)
					signalSent <- err
					return
				}
				signalSent <- syscall.Kill(os.Getpid(), sig)
			}()

			provider := Provider{ID: "signal-test", Name: "Signal Test", OpenAIURL: "https://example.com", Key: "secret"}
			err := launchCodex(&provider, "model", false, false, signalHelperArgs())
			exit, ok := err.(interface{ ExitCode() int })
			if !ok || exit.ExitCode() != 128+int(syscall.SIGKILL) {
				t.Fatalf("forced child exit = %#v", err)
			}
			if err := <-signalSent; err != nil {
				t.Fatal(err)
			}
			assertForwardedSignal(t, first, sig)
			assertCapturedDirRemoved(t, capture)
		})
	}
}

func signalHelperArgs() []string {
	return []string{"-test.run=^TestMuxLMSignalHelperProcess$"}
}

func installSignalHelper(t *testing.T, mode string) (capture, ready, first string) {
	t.Helper()
	root := isolatedConfig(t)
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(executable, filepath.Join(bin, "codex")); err != nil {
		t.Fatal(err)
	}
	capture = filepath.Join(root, "capture")
	ready = filepath.Join(root, "ready")
	first = filepath.Join(root, "first")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(signalHelperEnv, mode)
	t.Setenv("SIGNAL_HELPER_CAPTURE", capture)
	t.Setenv("SIGNAL_HELPER_READY", ready)
	t.Setenv("SIGNAL_HELPER_FIRST", first)
	return capture, ready, first
}

func waitForSignalFile(path string) error {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for signal fixture %s", filepath.Base(path))
}

func assertForwardedSignal(t *testing.T, path string, want syscall.Signal) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || got != int(want) {
		t.Fatalf("forwarded signal = %q, want %d (parse error: %v)", b, want, err)
	}
}
