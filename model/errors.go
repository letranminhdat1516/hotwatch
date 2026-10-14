package model

// APIError is the agent's error body. Codes: unauthorized, forbidden_read_only,
// not_found, bad_request, conflict, docker_unavailable, internal.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e APIError) Error() string { return e.Code + ": " + e.Message }
