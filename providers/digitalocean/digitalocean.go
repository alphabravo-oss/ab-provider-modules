package digitalocean

import (
	"encoding/json"
	"fmt"

	"github.com/digitalocean/godo"

	"github.com/alphabravo-oss/ab-provider-modules/pkg/resource"
)

func DiscoverJSON(raw []byte) ([]resource.Resource, error) {
	var wrap struct {
		Droplets []godo.Droplet `json:"droplets"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, err
	}
	out := []resource.Resource{{
		Type: "cloud_account", UID: "acct", Account: "acct",
		Aliases: []resource.Alias{{Kind: "do_account", Value: "acct"}},
	}}
	for _, d := range wrap.Droplets {
		uid := fmt.Sprintf("droplet:%d", d.ID)
		backup := len(d.BackupIDs) > 0 || d.NextBackupWindow != nil
		out = append(out, resource.Resource{
			Type: "digitalocean.resource", UID: uid, Account: "acct",
			Aliases: []resource.Alias{{Kind: "cloud_arn", Value: uid}},
			Attrs:   map[string]any{"backup_enabled": backup, "name": d.Name},
		})
	}
	return out, nil
}
