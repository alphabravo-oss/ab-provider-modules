package linode

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/linode/linodego"
	"golang.org/x/oauth2"

	"github.com/alphabravo-oss/ab-provider-modules/pkg/resource"
)

type LiveOpts struct {
	Token  string
	Client *linodego.Client
}

func DiscoverLive(ctx context.Context, options LiveOpts) ([]resource.Resource, []string, error) {
	if options.Token == "" && options.Client == nil {
		return nil, nil, fmt.Errorf("Linode token required")
	}
	client := options.Client
	if client == nil {
		httpClient := oauth2.NewClient(ctx, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: options.Token}))
		created := linodego.NewClient(httpClient)
		created.SetRetryCount(4).SetRetryWaitTime(500 * time.Millisecond).SetRetryMaxWaitTime(5 * time.Second)
		client = &created
	}
	account, err := client.GetAccount(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("Linode account: %w", err)
	}
	accountID := account.EUUID
	if accountID == "" {
		return nil, nil, fmt.Errorf("Linode account returned no stable EUUID")
	}
	out := []resource.Resource{{Type: "cloud_account", UID: accountID, Account: accountID, Aliases: []resource.Alias{{Kind: "linode_account_id", Value: accountID}}, Attrs: map[string]any{"capabilities": account.Capabilities}}}
	instances, err := client.ListInstances(ctx, &linodego.ListOptions{PageSize: 500})
	if err != nil {
		return out, []string{"Linode instances: " + err.Error()}, nil
	}
	for _, instance := range instances {
		uid := fmt.Sprintf("linode:instance:%d", instance.ID)
		publicIPv4 := make([]string, 0, len(instance.IPv4))
		for _, address := range instance.IPv4 {
			if address != nil && !address.IsPrivate() {
				publicIPv4 = append(publicIPv4, address.String())
			}
		}
		out = append(out, resource.Resource{Type: "linode.resource", UID: uid, Account: accountID, Region: instance.Region,
			Aliases: []resource.Alias{{Kind: "linode_instance_id", Value: fmt.Sprintf("%d", instance.ID)}},
			Attrs:   map[string]any{"name": instance.Label, "status": instance.Status, "public_ipv4": publicIPv4, "publicly_exposed": len(publicIPv4) > 0, "encryption_at_rest": string(instance.DiskEncryption) == "enabled", "backup_enabled": instance.Backups != nil && instance.Backups.Enabled, "tags": instance.Tags},
			Edges:   []resource.Edge{{From: accountID, To: uid, Kind: "contains"}},
		})
	}
	return out, []string{"Linode preview currently covers account and compute instance inventory only"}, nil
}

func ClientWithHTTP(client *http.Client) linodego.Client { return linodego.NewClient(client) }
