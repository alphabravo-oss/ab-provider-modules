package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/alphabravo-oss/ab-provider-modules/pkg/protocol"
	"github.com/alphabravo-oss/ab-provider-modules/pkg/resource"
	"github.com/alphabravo-oss/ab-provider-modules/providers/digitalocean"
)

func main() { protocol.Serve("digitalocean", "2.0.0", discover) }

func discover(ctx context.Context, raw json.RawMessage) ([]resource.Resource, []string, error) {
	var config struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(raw, &config); err != nil || config.Token == "" {
		return nil, nil, fmt.Errorf("valid DigitalOcean configuration required")
	}
	return digitalocean.DiscoverLive(ctx, digitalocean.LiveOpts{Token: config.Token})
}
