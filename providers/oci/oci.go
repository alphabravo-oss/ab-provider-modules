package oci

import (
	"encoding/json"

	"github.com/oracle/oci-go-sdk/v65/core"

	"github.com/alphabravo-oss/ab-provider-modules/pkg/resource"
)

func DiscoverJSON(raw []byte) ([]resource.Resource, error) {
	var resp core.ListInstancesResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	out := []resource.Resource{{
		Type: "cloud_account", UID: "tenancy", Account: "tenancy",
		Aliases: []resource.Alias{{Kind: "oci_tenancy", Value: "tenancy"}},
	}}
	for _, inst := range resp.Items {
		if inst.Id == nil {
			continue
		}
		uid := *inst.Id
		out = append(out, resource.Resource{
			Type: "oci.resource", UID: uid, Account: "tenancy",
			Aliases: []resource.Alias{{Kind: "cloud_arn", Value: uid}},
			Attrs:   map[string]any{"lifecycle": inst.LifecycleState},
		})
	}
	return out, nil
}
