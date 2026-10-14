// Package source defines what the hub and UI consume; implemented by the
// local collector adapter and the remote agent client.
package source

import (
	"context"
	"time"

	"hotwatch/model"
)

type Source interface {
	Info(ctx context.Context) (model.HostInfo, error)
	Snapshot(ctx context.Context) (model.Snapshot, error)
	Processes(ctx context.Context) ([]model.Process, error)
	Signal(ctx context.Context, pid int32, start time.Time, sig model.Signal) error // SigTerm, SigKill
	Containers(ctx context.Context) ([]model.Container, error)
	ContainerAction(ctx context.Context, id string, act model.ContainerAction) error // Start, Stop, Restart
	ContainerLogs(ctx context.Context, id string, tail int, follow bool) (<-chan model.LogLine, error)
}
