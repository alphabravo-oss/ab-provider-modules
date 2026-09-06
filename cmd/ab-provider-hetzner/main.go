package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/alphabravo-oss/ab-provider-modules/pkg/protocol"
	"github.com/alphabravo-oss/ab-provider-modules/pkg/resource"
	"github.com/alphabravo-oss/ab-provider-modules/providers/hetzner"
)

func main() { protocol.Serve("hetzner", "2.0.0", discover) }

func discover(ctx context.Context, raw json.RawMessage) ([]resource.Resource, []string, error) {
	var config struct{ Token, ProjectID string }
	if err := json.Unmarshal(raw, &config); err != nil || config.Token == "" || config.ProjectID == "" {
		return nil, nil, fmt.Errorf("valid Hetzner token and projectId required")
	}
	return hetzner.DiscoverLive(ctx, hetzner.LiveOpts{Token: config.Token, ProjectID: config.ProjectID})
}
