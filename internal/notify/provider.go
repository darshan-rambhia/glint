package notify

import (
	"context"

	"github.com/darshan-rambhia/glint/internal/model"
)

// Provider sends notifications through a specific channel.
type Provider interface {
	Name() string
	Send(ctx context.Context, n model.Notification) error
}
