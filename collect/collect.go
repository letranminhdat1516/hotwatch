// Package collect samples local host metrics; one Collector per OS.
package collect

import (
	"context"

	"hotwatch/model"
)

type Collector interface {
	Sample(ctx context.Context) (model.Snapshot, error) // deltas vs previous call
	Processes(ctx context.Context) ([]model.Process, error)
}
