package hetzner

import (
	"context"
	"fmt"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"

	"github.com/alphabravo-oss/ab-provider-modules/pkg/resource"
)

type LiveOpts struct {
	Token, ProjectID, Endpoint string
	Client                     *hcloud.Client
}

func DiscoverLive(ctx context.Context, options LiveOpts) ([]resource.Resource, []string, error) {
	if options.ProjectID == "" {
		return nil, nil, fmt.Errorf("Hetzner projectId required")
	}
	if options.Token == "" && options.Client == nil {
		return nil, nil, fmt.Errorf("Hetzner token required")
	}
	client := options.Client
	if client == nil {
		clientOptions := []hcloud.ClientOption{hcloud.WithToken(options.Token)}
		if options.Endpoint != "" {
			clientOptions = append(clientOptions, hcloud.WithEndpoint(options.Endpoint))
		}
		client = hcloud.NewClient(clientOptions...)
	}
	out := []resource.Resource{{Type: "cloud_account", UID: options.ProjectID, Account: options.ProjectID, Aliases: []resource.Alias{{Kind: "hetzner_project_id", Value: options.ProjectID}}}}
	servers, err := ListServers(ctx, client, 100000)
	if err != nil {
		return out, []string{"Hetzner servers: " + err.Error()}, nil
	}
	for _, server := range servers {
		uid := fmt.Sprintf("hetzner:server:%d", server.ID)
		region := ""
		if server.Location != nil {
			region = server.Location.Name
		}
		publicIPv4 := server.PublicNet.IPv4.IP.String()
		if server.PublicNet.IPv4.IsUnspecified() {
			publicIPv4 = ""
		}
		out = append(out, resource.Resource{Type: "hetzner.resource", UID: uid, Account: options.ProjectID, Region: region,
			Aliases: []resource.Alias{{Kind: "hetzner_server_id", Value: fmt.Sprintf("%d", server.ID)}},
			Attrs:   map[string]any{"name": server.Name, "status": server.Status, "public_ipv4": publicIPv4, "publicly_exposed": publicIPv4 != "", "backup_enabled": server.BackupWindow != "", "rescue_enabled": server.RescueEnabled, "delete_protection": server.Protection.Delete, "rebuild_protection": server.Protection.Rebuild, "labels": server.Labels},
			Edges:   []resource.Edge{{From: options.ProjectID, To: uid, Kind: "contains"}},
		})
	}
	return out, []string{"Hetzner preview currently covers project and server inventory only"}, nil
}

// ListServers returns a complete, bounded server inventory. Any failed page
// discards earlier rows so callers cannot mistake partial inventory for deletion.
func ListServers(ctx context.Context, client *hcloud.Client, limit int) ([]*hcloud.Server, error) {
	if client == nil || limit <= 0 {
		return nil, fmt.Errorf("client and positive resource limit required")
	}
	opts := hcloud.ServerListOpts{ListOpts: hcloud.ListOpts{Page: 1, PerPage: 50}}
	var servers []*hcloud.Server
	for {
		rows, response, err := client.Server.List(ctx, opts)
		if err != nil {
			return nil, err
		}
		if len(rows) > limit-len(servers) {
			return nil, fmt.Errorf("resource limit")
		}
		servers = append(servers, rows...)
		if response == nil || response.Meta.Pagination == nil {
			return nil, fmt.Errorf("missing pagination metadata")
		}
		next := response.Meta.Pagination.NextPage
		if next == 0 {
			return servers, nil
		}
		if next <= opts.Page {
			return nil, fmt.Errorf("invalid pagination")
		}
		opts.Page = next
	}
}
