package dockerdiscovery

import "testing"

func TestPublishedTargetHostForPodmanInContainer(t *testing.T) {
	t.Parallel()

	host := publishedTargetHost(true, true)
	if host != "host.containers.internal" {
		t.Fatalf("expected host.containers.internal, got %q", host)
	}
}

func TestPublishedTargetHostForPodmanOnHost(t *testing.T) {
	t.Parallel()

	host := publishedTargetHost(true, false)
	if host != "127.0.0.1" {
		t.Fatalf("expected 127.0.0.1, got %q", host)
	}
}

func TestPublishedTargetHostForNonPodman(t *testing.T) {
	t.Parallel()

	host := publishedTargetHost(false, true)
	if host != "127.0.0.1" {
		t.Fatalf("expected 127.0.0.1, got %q", host)
	}
}
