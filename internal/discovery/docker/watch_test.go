package dockerdiscovery

import (
	"testing"

	dockerevents "github.com/docker/docker/api/types/events"
)

func TestShouldRefreshOnEvent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		msg  dockerevents.Message
		want bool
	}{
		{name: "start", msg: dockerevents.Message{Type: dockerevents.ContainerEventType, Action: "start"}, want: true},
		{name: "rename", msg: dockerevents.Message{Type: dockerevents.ContainerEventType, Action: "rename"}, want: true},
		{name: "exec", msg: dockerevents.Message{Type: dockerevents.ContainerEventType, Action: "exec_start"}, want: false},
		{name: "network type", msg: dockerevents.Message{Type: "network", Action: "connect"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldRefreshOnEvent(tt.msg); got != tt.want {
				t.Fatalf("shouldRefreshOnEvent() = %v, want %v", got, tt.want)
			}
		})
	}
}
