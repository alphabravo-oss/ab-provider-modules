package hetzner

import (
	"encoding/json"
	"fmt"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"

	"github.com/alphabravo-oss/ab-provider-modules/pkg/resource"
)

func DiscoverJSON(raw []byte) ([]resource.Resource, error) {
	var wrap struct {
		Servers []struct {
			ID            int64  `json:"id"`
			Name          string `json:"name"`
			BackupWindow  string `json:"backup_window"`
			RescueEnabled bool   `json:"rescue_enabled"`
		} `json:"servers"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, err
	}
	out := []resource.Resource{{
		Type: "cloud_account", UID: "proj", Account: "proj",
		Aliases: []resource.Alias{{Kind: "hetzner_project", Value: "proj"}},
	}}
	for _, row := range wrap.Servers {
		srv := hcloud.Server{ID: row.ID, Name: row.Name, BackupWindow: row.BackupWindow, RescueEnabled: row.RescueEnabled}
		uid := fmt.Sprintf("server:%d", srv.ID)
		out = append(out, resource.Resource{
			Type: "hetzner.resource", UID: uid, Account: "proj",
			Aliases: []resource.Alias{{Kind: "cloud_arn", Value: uid}},
			Attrs: map[string]any{
				"backup_enabled": srv.BackupWindow != "",
				"name":           srv.Name,
			},
		})
	}
	return out, nil
}
