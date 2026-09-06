package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alphabravo-oss/ab-provider-modules/pkg/protocol"
	"github.com/alphabravo-oss/ab-provider-modules/pkg/resource"
	"github.com/alphabravo-oss/ab-provider-modules/providers/gcp"
)

func main() { protocol.Serve("gcp", "2.0.0", discover) }

func discover(ctx context.Context, raw json.RawMessage) ([]resource.Resource, []string, error) {
	var config struct {
		ProjectIDs         string `json:"projectIds"`
		ServiceAccountJSON string `json:"serviceAccountJson"`
	}
	if err := json.Unmarshal(raw, &config); err != nil || strings.TrimSpace(config.ProjectIDs) == "" || strings.TrimSpace(config.ServiceAccountJSON) == "" {
		return nil, nil, fmt.Errorf("valid GCP projectIds and serviceAccountJson required")
	}
	projects := []string{}
	for _, project := range strings.Split(config.ProjectIDs, ",") {
		if project = strings.TrimSpace(project); project != "" {
			projects = append(projects, project)
		}
	}
	return gcp.DiscoverLive(ctx, gcp.LiveOpts{ProjectIDs: projects, ServiceAccountJSON: []byte(config.ServiceAccountJSON)})
}
