//go:build !windows

package portable

import (
	"context"

	"hotwatch/model"
)

// Battery is nil, nil off Windows: no portable source.
func Battery(context.Context) (*model.Battery, error) { return nil, nil }
