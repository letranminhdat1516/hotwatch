//go:build darwin

package power

import (
	"context"
	"os/exec"
	"reflect"
	"testing"
	"time"

	"hotwatch/model"
)

func TestAssertionsSeesCaffeinate(t *testing.T) {
	cmd := exec.Command("caffeinate", "-i", "-t", "5")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start caffeinate: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	pid := int32(cmd.Process.Pid)

	deadline := time.Now().Add(1500 * time.Millisecond)
	for {
		list, err := Assertions(context.Background())
		if err != nil {
			t.Fatalf("Assertions: %v", err)
		}
		for _, a := range list {
			if a.PID == pid {
				if a.Process != "caffeinate" || a.Type != "PreventUserIdleSystemSleep" || a.Reason == "" {
					t.Fatalf("got %+v", a)
				}
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("caffeinate pid %d not in %+v", pid, list)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestFromAssertions(t *testing.T) {
	got := fromAssertions(map[string]any{
		"402": []any{map[string]any{
			"AssertPID": int64(402), "Process Name": "coreaudiod",
			"AssertType": "PreventUserIdleSystemSleep", "AssertName": "speaker",
			"AssertionOnBehalfOfPID": int64(75014),
		}},
		"332": []any{
			map[string]any{"AssertPID": int64(332), "Process Name": "powerd", "AssertType": "UserIsActive", "AssertName": "lid"},
			map[string]any{"AssertPID": int64(332), "Process Name": "powerd", "AssertType": "PreventSystemSleep", "AssertName": "b"},
			map[string]any{"AssertPID": int64(332), "Process Name": "powerd", "AssertType": "PreventUserIdleDisplaySleep", "AssertName": "a"},
		},
		"7": []any{map[string]any{
			"AssertPID": int64(7), "Process Name": "legacy",
			"AssertType": "NoIdleSleepAssertion", "AssertionTrueType": "PreventUserIdleSystemSleep",
			"AssertName": "old api",
		}},
		"junk": "not a list",
	})
	want := []model.Assertion{
		{PID: 7, Process: "legacy", Type: "NoIdleSleepAssertion", Reason: "old api"},
		{PID: 332, Process: "powerd", Type: "PreventSystemSleep", Reason: "b"},
		{PID: 332, Process: "powerd", Type: "PreventUserIdleDisplaySleep", Reason: "a"},
		{PID: 402, Process: "coreaudiod", Type: "PreventUserIdleSystemSleep", Reason: "speaker (for PID 75014)"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}
