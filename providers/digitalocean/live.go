package digitalocean

import (
	"context"
	"fmt"
	"net/http"

	"github.com/digitalocean/godo"

	"github.com/alphabravo-oss/ab-provider-modules/pkg/resource"
)

type LiveOpts struct {
	Token  string
	Client *godo.Client
}

func DiscoverLive(ctx context.Context, options LiveOpts) ([]resource.Resource, []string, error) {
	if options.Token == "" && options.Client == nil {
		return nil, nil, fmt.Errorf("DigitalOcean token required")
	}
	client := options.Client
	if client == nil {
		client = godo.NewFromToken(options.Token)
	}
	account, _, err := client.Account.Get(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("DigitalOcean account: %w", err)
	}
	accountID := account.UUID
	if account.Team != nil && account.Team.UUID != "" {
		accountID = account.Team.UUID
	}
	if accountID == "" {
		return nil, nil, fmt.Errorf("DigitalOcean account returned no stable UUID")
	}
	out := []resource.Resource{{Type: "cloud_account", UID: accountID, Account: accountID, Aliases: []resource.Alias{{Kind: "digitalocean_account_id", Value: accountID}}, Attrs: map[string]any{"status": account.Status, "email_verified": account.EmailVerified}}}
	for page := 1; ; page++ {
		droplets, response, listErr := client.Droplets.List(ctx, &godo.ListOptions{Page: page, PerPage: 200})
		if listErr != nil {
			return out, []string{"DigitalOcean droplets: " + listErr.Error()}, nil
		}
		for _, droplet := range droplets {
			uid := fmt.Sprintf("digitalocean:droplet:%d", droplet.ID)
			publicIPv4, _ := droplet.PublicIPv4()
			region := ""
			if droplet.Region != nil {
				region = droplet.Region.Slug
			}
			out = append(out, resource.Resource{Type: "digitalocean.resource", UID: uid, Account: accountID, Region: region,
				Aliases: []resource.Alias{{Kind: "digitalocean_droplet_id", Value: fmt.Sprintf("%d", droplet.ID)}},
				Attrs:   map[string]any{"name": droplet.Name, "status": droplet.Status, "public_ipv4": publicIPv4, "publicly_exposed": publicIPv4 != "", "backup_enabled": len(droplet.BackupIDs) > 0 || droplet.NextBackupWindow != nil, "tags": droplet.Tags},
				Edges:   []resource.Edge{{From: accountID, To: uid, Kind: "contains"}},
			})
		}
		if response == nil || response.Links == nil || response.Links.IsLastPage() || len(droplets) == 0 {
			break
		}
	}
	return out, []string{"DigitalOcean preview currently covers account and Droplet inventory only"}, nil
}

func ClientWithHTTP(token string, client *http.Client) *godo.Client {
	if client == nil {
		return godo.NewFromToken(token)
	}
	return godo.NewClient(client)
}
