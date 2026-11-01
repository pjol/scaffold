//go:build unix

package repos

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// The pre-receive check. git receive-pack runs the pre-receive hook after
// the pack arrives (objects still in quarantine) and before any ref moves;
// a non-zero exit refuses the whole push and git shows the hook's stderr to
// the person as "remote: …" lines. Our hook is a few lines of POSIX sh
// (written at boot, see preReceiveScript) that relays the ref updates to
// this process through a FIFO pair made for the request, and relays the
// verdict back. The decision runs here, in Go, with the database.

// preReceiveScript needs only sh and cat.
const preReceiveScript = `#!/bin/sh
# Written by the repos feature at boot. The API decides whether a push may
# land; this relays the ref updates to it and its answer back.
dir="$REPOS_HOOK_DIR"
if [ -z "$dir" ] || [ ! -p "$dir/in" ] || [ ! -p "$dir/out" ]; then
  echo "Push through the app's git address." >&2
  exit 1
fi
{ printf 'quarantine %s\n' "$GIT_QUARANTINE_PATH"; cat; } > "$dir/in"
verdict=deny
{ read -r verdict; cat >&2; } < "$dir/out"
[ "$verdict" = ok ]
`

const hookAnswerTimeout = 30 * time.Second

// pushHook is one push's FIFO pair and the goroutine answering it.
type pushHook struct {
	s        *Service
	repo     *Repo
	dir      string
	done     chan struct{}
	accepted []refUpdate
}

func (s *Service) newPushHook(ctx context.Context, repo *Repo) (*pushHook, error) {
	dir, err := os.MkdirTemp(s.tmpDir, "push-")
	if err != nil {
		return nil, err
	}
	for _, name := range []string{"in", "out"} {
		if err := syscall.Mkfifo(filepath.Join(dir, name), 0o600); err != nil {
			_ = os.RemoveAll(dir)
			return nil, err
		}
	}
	h := &pushHook{s: s, repo: repo, dir: dir, done: make(chan struct{})}
	go h.serve(context.WithoutCancel(ctx))
	return h, nil
}

func (h *pushHook) env() []string { return []string{"REPOS_HOOK_DIR=" + h.dir} }

// serve answers the hook once: read the updates, decide, write the verdict.
func (h *pushHook) serve(ctx context.Context) {
	defer close(h.done)
	in, err := os.OpenFile(filepath.Join(h.dir, "in"), os.O_RDONLY, 0) // waits for the hook (or close)
	if err != nil {
		return
	}
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(io.LimitReader(in, 1<<20))
	_ = in.Close()
	if buf.Len() == 0 {
		return // the push ended without running the hook
	}
	quarantine, updates := parseHookInput(buf.Bytes())
	verdict := "ok\n"
	if len(updates) > 0 {
		dctx, cancel := context.WithTimeout(ctx, hookAnswerTimeout)
		env := []string{}
		if quarantine != "" {
			env = append(env, "GIT_OBJECT_DIRECTORY="+quarantine, "GIT_ALTERNATE_OBJECT_DIRECTORIES="+filepath.Join(h.s.git.Dir(h.repo.ID), "objects"))
		}
		denied, err := h.s.checkPush(dctx, h.repo, updates, env)
		cancel()
		switch {
		case err != nil:
			slog.ErrorContext(ctx, "repos: push check failed", "repo_id", h.repo.ID, "err", err)
			verdict = "deny\nThe server couldn't check this push. Try again.\n"
		case len(denied) > 0:
			verdict = "deny\n" + strings.Join(denied, "\n") + "\n"
		default:
			h.accepted = updates
		}
	}
	out, err := os.OpenFile(filepath.Join(h.dir, "out"), os.O_WRONLY, 0) // waits for the hook to read
	if err != nil {
		h.accepted = nil
		return
	}
	if _, err := out.WriteString(verdict); err != nil {
		h.accepted = nil
	}
	_ = out.Close()
}

// wait returns the updates the check accepted (receive-pack has exited by
// now, so they landed unless git failed afterwards: callers re-read refs).
func (h *pushHook) wait() []refUpdate {
	h.unblock()
	return h.accepted
}

// unblock frees the goroutine if the hook never ran or died midway, then
// waits for it.
func (h *pushHook) unblock() {
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case <-h.done:
			return
		default:
		}
		if time.Now().After(deadline) {
			slog.Warn("repos: push check goroutine didn't finish", "repo_id", h.repo.ID)
			return
		}
		// A non-blocking open succeeds only while the other side waits.
		if f, err := os.OpenFile(filepath.Join(h.dir, "in"), os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			_ = f.Close()
		}
		if f, err := os.OpenFile(filepath.Join(h.dir, "out"), os.O_RDONLY|syscall.O_NONBLOCK, 0); err == nil {
			_ = f.Close()
		}
		select {
		case <-h.done:
			return
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func (h *pushHook) close() {
	h.unblock()
	_ = os.RemoveAll(h.dir)
}

// parseHookInput reads "quarantine <path>" then "<old> <new> <ref>" lines.
func parseHookInput(b []byte) (string, []refUpdate) {
	var quarantine string
	var updates []refUpdate
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if q, ok := strings.CutPrefix(line, "quarantine "); ok {
			quarantine = strings.TrimSpace(q)
			continue
		}
		f := strings.Fields(line)
		if len(f) == 3 && IsOID(f[0]) && IsOID(f[1]) {
			updates = append(updates, refUpdate{Old: f[0], New: f[1], Ref: f[2]})
		}
	}
	return quarantine, updates
}

// writeHookScript installs the pre-receive script in the hooks directory.
func writeHookScript(dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	p := filepath.Join(dir, "pre-receive")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(preReceiveScript), 0o750); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// pushSupported: Unix hosts can push.
const pushSupported = true
