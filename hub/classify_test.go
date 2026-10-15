package hub

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"hotwatch/model"
	"hotwatch/transport"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want Event
	}{
		{"nil", nil, EvOK},
		{"unauthorized", &model.APIError{Code: "unauthorized"}, EvUnauthorized},
		{"wrapped unauthorized", fmt.Errorf("x: %w", error(&model.APIError{Code: "unauthorized"})), EvUnauthorized},
		{"unavailable", &model.APIError{Code: "unavailable"}, EvFail},
		{"other api", &model.APIError{Code: "internal"}, EvFail},
		{"unknown key", &transport.HostKeyError{Host: "h"}, EvHostKeyUnknown},
		{"mismatch", fmt.Errorf("dial: %w", &transport.HostKeyError{Host: "h", Mismatch: true}), EvHostKeyMismatch},
		{"incompatible", &model.IncompatibleError{Version: "2.0"}, EvIncompatible},
		{"plain", errors.New("boom"), EvFail},
		{"canceled", context.Canceled, EvFail},
	}
	for _, c := range cases {
		if got := Classify(c.err); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
