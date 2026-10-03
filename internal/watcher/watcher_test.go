package watcher

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"forge.red.bloomyindev.me/bastien/my-usage/internal/accounts"
)

func signalled(ch <-chan struct{}, within time.Duration) bool {
	select {
	case <-ch:
		return true
	case <-time.After(within):
		return false
	}
}

func TestWatch(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := Watch(ctx, dir, 20*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	// A temp file and a foreign file are not account changes.
	os.WriteFile(filepath.Join(dir, ".a.yaml.1.tmp"), []byte("x"), 0o600)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o600)
	if signalled(ch, 200*time.Millisecond) {
		t.Fatal("signalled for a non-account file")
	}

	// Two atomic replaces in a row: the inode changes each time and the
	// directory watch must still see the second one.
	path := filepath.Join(dir, "a.yaml")
	for _, body := range []string{"one", "two"} {
		if err := accounts.WriteAtomic(path, []byte(body)); err != nil {
			t.Fatal(err)
		}
		if !signalled(ch, 2*time.Second) {
			t.Fatalf("no signal after writing %q", body)
		}
	}

	cancel()
	for range ch { // drains until closed
	}
}
