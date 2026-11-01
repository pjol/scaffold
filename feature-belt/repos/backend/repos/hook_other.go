//go:build !unix

package repos

import (
	"context"
	"errors"
)

// Pushing needs FIFOs for the pre-receive check (hook_unix.go); other hosts
// can clone and fetch but refuse pushes.

const pushSupported = false

type pushHook struct{}

func (s *Service) newPushHook(context.Context, *Repo) (*pushHook, error) {
	return nil, errors.New("repos: pushing needs a Unix host")
}

func (h *pushHook) env() []string     { return nil }
func (h *pushHook) wait() []refUpdate { return nil }
func (h *pushHook) close()            {}
func writeHookScript(string) error    { return nil }
