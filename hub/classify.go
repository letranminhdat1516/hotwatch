package hub

import (
	"errors"

	"hotwatch/model"
	"hotwatch/transport"
)

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
	var inc *model.IncompatibleError
	if errors.As(err, &inc) {
		return EvIncompatible
	}
	var api *model.APIError
	if errors.As(err, &api) && api.Code == "unauthorized" {
		return EvUnauthorized
	}
	return EvFail
}
