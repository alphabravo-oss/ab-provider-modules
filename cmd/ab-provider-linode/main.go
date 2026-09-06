package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/alphabravo-oss/ab-provider-modules/pkg/protocol"
	"github.com/alphabravo-oss/ab-provider-modules/pkg/resource"
	"github.com/alphabravo-oss/ab-provider-modules/providers/linode"
)

func main() { protocol.Serve("linode", "2.0.0", discover) }

func discover(ctx context.Context, raw json.RawMessage) ([]resource.Resource, []string, error) {
	var config struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(raw, &config); err != nil || config.Token == "" {
		return nil, nil, fmt.Errorf("valid Linode configuration required")
	}
	return linode.DiscoverLive(ctx, linode.LiveOpts{Token: config.Token})
}
