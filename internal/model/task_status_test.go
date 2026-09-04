package model

import "testing"

func TestMapTaskStatusToScanStatus(t *testing.T) {
	cases := map[string]string{
		TaskStatusCreated: "pending",
		TaskStatusPending: "pending",
		TaskStatusPaused:  "pending",
		TaskStatusStarted: "in_progress",
		TaskStatusSuccess: "completed",
		TaskStatusPartial: "completed",
		TaskStatusFailure: "failed",
		TaskStatusRevoked: "cancelled",
		TaskStatusStopped: "cancelled",
		"UNKNOWN":         "",
		"":                "",
	}
	for in, want := range cases {
		if got := mapTaskStatusToScanStatus(in); got != want {
			t.Errorf("mapTaskStatusToScanStatus(%q) = %q, want %q", in, got, want)
		}
	}
}
