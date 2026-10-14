//go:build darwin

package power

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"hotwatch/internal/cf"
	"hotwatch/model"
)

// sleepTypes are the assertion types that keep the Mac (or its display)
// awake. NoIdleSleepAssertion and NoDisplaySleepAssertion are the legacy
// names of the PreventUserIdle* types.
var sleepTypes = map[string]bool{
	"PreventUserIdleSystemSleep":  true,
	"PreventUserIdleDisplaySleep": true,
	"PreventSystemSleep":          true,
	"NoIdleSleepAssertion":        true,
	"NoDisplaySleepAssertion":     true,
}

// Assertions lists the power assertions that prevent sleep, from
// IOPMCopyAssertionsByProcess, sorted by PID then type.
func Assertions(ctx context.Context) ([]model.Assertion, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := load(); err != nil {
		return nil, err
	}
	var d cf.Ref
	if rc := ps.byProcess(&d); rc != 0 {
		return nil, fmt.Errorf("power: IOPMCopyAssertionsByProcess: IOReturn %#x", uint32(rc))
	}
	defer cf.Release(d)
	byPID, _ := cf.ToGo(d).(map[string]any)
	return fromAssertions(byPID), nil
}

// fromAssertions flattens the PID → [assertion dictionary] map. The type is
// the one the process asked for (AssertType); the filter also accepts the
// resolved AssertionTrueType. When a daemon holds an assertion on behalf of
// another process, that PID is kept in the reason.
func fromAssertions(byPID map[string]any) []model.Assertion {
	out := []model.Assertion{}
	for _, v := range byPID {
		list, _ := v.([]any)
		for _, item := range list {
			a, _ := item.(map[string]any)
			typ := cf.Str(a, "AssertType")
			if !sleepTypes[typ] && !sleepTypes[cf.Str(a, "AssertionTrueType")] {
				continue
			}
			pid, _ := cf.Int(a, "AssertPID")
			reason := cf.Str(a, "AssertName")
			if other, ok := cf.Int(a, "AssertionOnBehalfOfPID"); ok {
				reason = fmt.Sprintf("%s (for PID %d)", reason, other)
			}
			out = append(out, model.Assertion{
				PID:     int32(pid),
				Process: cf.Str(a, "Process Name"),
				Type:    typ,
				Reason:  reason,
			})
		}
	}
	slices.SortFunc(out, func(x, y model.Assertion) int {
		return cmp.Or(cmp.Compare(x.PID, y.PID), cmp.Compare(x.Type, y.Type), cmp.Compare(x.Reason, y.Reason))
	})
	return out
}
