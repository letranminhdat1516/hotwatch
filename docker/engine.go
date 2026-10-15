// Package docker defines the container-engine contract used by the agent and
// hub, plus test fakes.
package docker

import (
	"context"

	"hotwatch/model"
)

// Engine is the Docker Engine API subset hotwatch needs.
type Engine interface {
	// List returns all containers, including CPU/memory stats.
	List(ctx context.Context) ([]model.Container, error)
	// Do runs a start, stop or restart action on a container.
	Do(ctx context.Context, id string, act model.ContainerAction) error
	// Logs streams log lines; the channel closes at end of stream or when ctx is done.
	Logs(ctx context.Context, id string, tail int, follow bool) (<-chan model.LogLine, error)
}
