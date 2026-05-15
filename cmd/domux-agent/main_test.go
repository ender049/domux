package main

import (
	"testing"

	"domux/internal/core"
)

func TestDefaultSocketPathUsesPodmanRuntimeDefaults(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/tmp/jd-podman")

	if got := defaultSocketPath(core.ContainerRuntimePodman); got != "/tmp/jd-podman/podman/podman.sock" {
		t.Fatalf("unexpected podman socket path: %q", got)
	}
}
