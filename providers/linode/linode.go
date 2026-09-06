package linode

import (
	"encoding/json"
	"fmt"

	"github.com/linode/linodego"

	"github.com/alphabravo-oss/ab-provider-modules/pkg/resource"
)

func DiscoverJSON(raw []byte) ([]resource.Resource, error) {
	var inst []linodego.Instance
	if err := json.Unmarshal(raw, &inst); err != nil {
		return nil, err
	}
	out := []resource.Resource{{
		Type: "cloud_account", UID: "acct", Account: "acct",
		Aliases: []resource.Alias{{Kind: "linode_account", Value: "acct"}},
	}}
	for _, i := range inst {
		uid := fmt.Sprintf("linode:%d", i.ID)
		enc := string(i.DiskEncryption) == "enabled"
		backup := i.Backups != nil && i.Backups.Enabled
		out = append(out, resource.Resource{
			Type: "linode.resource", UID: uid, Account: "acct",
			Aliases: []resource.Alias{{Kind: "cloud_arn", Value: uid}},
			Attrs: map[string]any{
				"encryption_at_rest": enc, "backup_enabled": backup,
			},
		})
	}
	return out, nil
}
