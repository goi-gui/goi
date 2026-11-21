//go:build !js && !wasip1

package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"time"
)

// runAutoProject keeps a go run alive while an auto flag set is in use. It
// watches the .goi files, re-scans the features on a change, and restarts the
// child only when the feature tags differ; ordinary design edits stay with the
// app's own hot reload.
func runAutoProject(dir string, cfg config, flags resolvedFlags, stdout, stderr io.Writer) error {
	assets := filepath.Join(dir, filepath.FromSlash(cfg.Assets))
	snapshot := goiSnapshot(assets)

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	defer signal.Stop(signals)
	interrupted := make(chan struct{})
	var interruptedOnce sync.Once
	var mu sync.Mutex
	var current *exec.Cmd
	go func() {
		<-signals
		interruptedOnce.Do(func() { close(interrupted) })
		mu.Lock()
		cmd := current
		mu.Unlock()
		if cmd != nil {
			terminateProcess(cmd)
		}
	}()

	for {
		select {
		case <-interrupted:
			return nil
		default:
		}
		cmd := exec.Command("go", runArgs(cfg, flags)...)
		cmd.Dir = dir
		cmd.Env = runEnv()
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, stdout, stderr
		setProcessGroup(cmd)
		if err := cmd.Start(); err != nil {
			return err
		}
		mu.Lock()
		current = cmd
		mu.Unlock()

		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		ticker := time.NewTicker(300 * time.Millisecond)
		restart := false
		for !restart {
			select {
			case err := <-done:
				ticker.Stop()
				mu.Lock()
				current = nil
				mu.Unlock()
				select {
				case <-interrupted:
					return nil
				default:
				}
				return err
			case <-ticker.C:
				next := goiSnapshot(assets)
				if snapshotsEqual(snapshot, next) {
					continue
				}
				snapshot = next
				updated, err := resolveFlags(dir, cfg.Assets, cfg.RunFlags)
				if err != nil {
					fmt.Fprintf(stderr, "goi: flags: %v\n", err)
					continue
				}
				if updated.FeatureKey == flags.FeatureKey {
					continue
				}
				fmt.Fprintln(stdout, "goi: design flags changed; restarting")
				logTags(stdout, "run", updated)
				flags = updated
				restart = true
			}
		}
		ticker.Stop()
		terminateProcess(cmd)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			killProcess(cmd)
			<-done
		}
		mu.Lock()
		current = nil
		mu.Unlock()
	}
}

// fileStamp is enough to notice a saved .goi without hashing a large asset
// tree every poll.
type fileStamp struct {
	mod  time.Time
	size int64
}

func goiSnapshot(dir string) map[string]fileStamp {
	out := map[string]fileStamp{}
	_ = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !isGoiFile(entry.Name()) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		out[path] = fileStamp{mod: info.ModTime(), size: info.Size()}
		return nil
	})
	return out
}

func snapshotsEqual(a, b map[string]fileStamp) bool {
	if len(a) != len(b) {
		return false
	}
	for path, stamp := range a {
		if b[path] != stamp {
			return false
		}
	}
	return true
}

func isGoiFile(name string) bool {
	return filepath.Ext(name) == ".goi"
}
