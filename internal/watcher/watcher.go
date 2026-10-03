// Package watcher signals when the accounts directory may have changed.
package watcher

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"

	"forge.red.bloomyindev.me/bastien/my-usage/internal/accounts"
)

// Watch watches the directory itself, not its files: an atomic write replaces
// the inode, and a per-file watch would be lost. Events on non-account files
// are dropped, and a burst of events is merged into one signal sent after
// debounce of quiet. The channel closes when ctx ends.
func Watch(ctx context.Context, dir string, debounce time.Duration) (<-chan struct{}, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create watcher: %w", err)
	}
	if err := w.Add(dir); err != nil {
		w.Close()
		return nil, fmt.Errorf("watch %s: %w", dir, err)
	}

	out := make(chan struct{}, 1)
	go func() {
		defer close(out)
		defer w.Close()

		timer := time.NewTimer(debounce)
		timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if accounts.IsAccountFile(filepath.Base(ev.Name)) {
					timer.Reset(debounce)
				}
			case <-w.Errors:
				// Events may have been lost: ask for a rescan.
				timer.Reset(debounce)
			case <-timer.C:
				select {
				case out <- struct{}{}:
				default: // a signal is already pending
				}
			}
		}
	}()
	return out, nil
}
