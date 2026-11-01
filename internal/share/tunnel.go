// Package share manages the host-only HTTPS connector. Guests use a browser.
package share

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

func BinaryName(goos, arch string) string {
	name := "cloudflared-" + goos + "-" + arch
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

func FindBinary() (string, error) {
	if value := os.Getenv("CLOUDFLARED_BINARY"); value != "" {
		return exec.LookPath(value)
	}
	path := filepath.Join("bin", BinaryName(runtime.GOOS, runtime.GOARCH))
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return filepath.Abs(path)
	}
	if path, err := exec.LookPath("cloudflared"); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("HTTPS connector is not installed on the host. Run: go run . --install-tunnel (guests install nothing)")
}

var tunnelURL = regexp.MustCompile(`(https://[a-z0-9]+(?:-[a-z0-9]+)*\.trycloudflare\.com)(?:[[:space:]|"'<>)]|$)`)

// ParseURL recognizes only the hostname allocated by the Quick Tunnel service.
func ParseURL(line string) string {
	if match := tunnelURL.FindStringSubmatch(line); len(match) == 2 {
		return match[1]
	}
	return ""
}

func Arguments(localURL, configPath string) []string {
	return []string{"tunnel", "--config", configPath, "--no-autoupdate", "--url", localURL, "--protocol", "http2"}
}

type Tunnel struct {
	Done <-chan error
	stop context.CancelFunc
}

func (t *Tunnel) Stop() { t.stop() }

// Start owns the child lifetime. A temporary empty config prevents a user's
// unrelated named-tunnel config from silently changing this game's destination.
func Start(ctx context.Context, binary, localURL string, onURL func(string), log io.Writer) (*Tunnel, error) {
	return start(ctx, binary, localURL, onURL, log, 90*time.Second)
}

func start(ctx context.Context, binary, localURL string, onURL func(string), log io.Writer, startupTimeout time.Duration) (*Tunnel, error) {
	config, err := os.CreateTemp("", "lastcrew-tunnel-*.yml")
	if err != nil {
		return nil, err
	}
	configPath := config.Name()
	if _, err = config.WriteString("{}\n"); err != nil {
		config.Close()
		os.Remove(configPath)
		return nil, err
	}
	if err = config.Close(); err != nil {
		os.Remove(configPath)
		return nil, err
	}
	childCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(childCtx, binary, Arguments(localURL, configPath)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		os.Remove(configPath)
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		os.Remove(configPath)
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		cancel()
		os.Remove(configPath)
		return nil, err
	}
	done := make(chan error, 1)
	ready := make(chan struct{})
	var readers sync.WaitGroup
	var output sync.Mutex
	var state sync.Mutex
	var address string
	var connected, announced bool
	var startupErr error
	scan := func(reader io.Reader) {
		defer readers.Done()
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 4096), 1<<20)
		for scanner.Scan() {
			line := scanner.Text()
			var notify string
			state.Lock()
			if startupErr == nil && childCtx.Err() == nil {
				if parsed := ParseURL(line); parsed != "" && address == "" {
					address = parsed
				}
				if strings.Contains(line, "Registered tunnel connection") {
					connected = true
				}
				if address != "" && connected && !announced {
					announced = true
					notify = address
					close(ready)
				}
			}
			state.Unlock()
			if notify != "" {
				onURL(notify)
			}
			if strings.Contains(line, "ERR") || strings.Contains(line, "WRN") {
				output.Lock()
				fmt.Fprintln(log, "HTTPS connector:", line)
				output.Unlock()
			}
		}
		if err := scanner.Err(); err != nil {
			state.Lock()
			if startupErr == nil {
				startupErr = fmt.Errorf("HTTPS connector output: %w", err)
			}
			state.Unlock()
			cancel()
		}
	}
	readers.Add(2)
	go scan(stdout)
	go scan(stderr)
	go func() {
		readers.Wait()
		err := cmd.Wait()
		cancel()
		os.Remove(configPath)
		state.Lock()
		if ctx.Err() != nil {
			err = nil
		} else if startupErr != nil {
			err = startupErr
		} else if !announced {
			if err == nil {
				err = fmt.Errorf("HTTPS connector exited before its public connection became ready")
			} else {
				err = fmt.Errorf("HTTPS connector exited before its public connection became ready: %w", err)
			}
		}
		state.Unlock()
		done <- err
		close(done)
	}()
	// A provisioned URL is not yet a working tunnel: keep the watchdog until the
	// connector has also registered its first edge connection.
	go func() {
		timer := time.NewTimer(startupTimeout)
		defer timer.Stop()
		select {
		case <-childCtx.Done():
			return
		case <-ready:
			return
		case <-timer.C:
			state.Lock()
			if announced || childCtx.Err() != nil {
				state.Unlock()
				return
			}
			startupErr = fmt.Errorf("HTTPS connector did not become ready within %s; check Internet access and connector logs", startupTimeout)
			message := startupErr.Error()
			state.Unlock()
			output.Lock()
			fmt.Fprintln(log, message)
			output.Unlock()
			cancel()
		}
	}()
	return &Tunnel{Done: done, stop: cancel}, nil
}
