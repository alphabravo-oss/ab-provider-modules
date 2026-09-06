package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alphabravo-oss/ab-provider-modules/pkg/protocol"
	"github.com/alphabravo-oss/ab-provider-modules/pkg/resource"
	"github.com/alphabravo-oss/ab-provider-modules/providers/azure"
)

func main() { protocol.Serve("azure", "2.0.0", discover) }

func discover(ctx context.Context, raw json.RawMessage) ([]resource.Resource, []string, error) {
	var config struct {
		TenantID, ClientID, ClientSecret string
		SubscriptionIDs                  string `json:"subscriptionIds"`
	}
	if err := json.Unmarshal(raw, &config); err != nil || config.TenantID == "" || config.ClientID == "" || config.ClientSecret == "" || config.SubscriptionIDs == "" {
		return nil, nil, fmt.Errorf("valid Azure tenantId, clientId, clientSecret, and subscriptionIds required")
	}
	subscriptions := []string{}
	for _, subscription := range strings.Split(config.SubscriptionIDs, ",") {
		if subscription = strings.TrimSpace(subscription); subscription != "" {
			subscriptions = append(subscriptions, subscription)
		}
	}
	return azure.DiscoverLive(ctx, azure.LiveOpts{TenantID: config.TenantID, ClientID: config.ClientID, ClientSecret: config.ClientSecret, SubscriptionIDs: subscriptions})
}
