// Package client re-exports the official Go SDK.
// Prefer importing github.com/devotionalist/open-message-client-go directly.
package client

import (
	"context"

	om "github.com/devotionalist/open-message-client-go"
)

type (
	Config   = om.Config
	Client   = om.Client
	Producer = om.Producer
	Consumer = om.Consumer
	Message  = om.Message
)

func Connect(ctx context.Context, cfg Config) (*Client, error) {
	return om.Connect(ctx, cfg)
}
