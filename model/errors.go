package model

// APIError is the agent's error body. Codes: unauthorized, forbidden, forbidden_read_only,
// not_found, too_many_requests, bad_request, conflict, docker_unavailable, internal.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e APIError) Error() string { return e.Code + ": " + e.Message }
