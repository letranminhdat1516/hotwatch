package hub

import (
	"errors"
	"fmt"

	"hotwatch/model"
	"hotwatch/transport"
)

// IncompatibleError reports an agent whose protocol major version differs.
type IncompatibleError struct{ Version string }

func (e *IncompatibleError) Error() string {
	return fmt.Sprintf("agent protocol %q is incompatible with %q", e.Version, model.ProtocolVersion)
}

// Classify maps a source error to the connection event it represents.
// nil is EvOK; every unrecognised error (including 503 "unavailable",
// timeouts and cancellation) is EvFail.
func Classify(err error) Event {
	if err == nil {
		return EvOK
	}
	var hk *transport.HostKeyError
	if errors.As(err, &hk) {
		if hk.Mismatch {
			return EvHostKeyMismatch
		}
		return EvHostKeyUnknown
	}
	var inc *IncompatibleError
	if errors.As(err, &inc) {
		return EvIncompatible
	}
	var api *model.APIError
	if errors.As(err, &api) && api.Code == "unauthorized" {
		return EvUnauthorized
	}
	return EvFail
}
