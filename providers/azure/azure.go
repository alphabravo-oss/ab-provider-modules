package azure

import (
	"encoding/json"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage"

	"github.com/alphabravo-oss/ab-provider-modules/pkg/resource"
)

func DiscoverJSON(raw []byte) ([]resource.Resource, error) {
	var list armresources.ResourceListResult
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	var extra struct {
		StorageGet map[string]armstorage.Account `json:"storageAccountsGet"`
	}
	_ = json.Unmarshal(raw, &extra)
	var out []resource.Resource
	seen := map[string]bool{}
	for _, g := range list.Value {
		if g == nil || g.ID == nil {
			continue
		}
		id := *g.ID
		sub := subFromID(id)
		if sub != "" && !seen[sub] {
			seen[sub] = true
			out = append(out, resource.Resource{
				Type: "cloud_account", UID: sub, Account: sub,
				Aliases: []resource.Alias{{Kind: "azure_subscription_id", Value: sub}},
			})
		}
		attrs := map[string]any{}
		if g.Name != nil {
			attrs["name"] = *g.Name
		}
		if g.Location != nil {
			attrs["location"] = *g.Location
		}
		typ := "azure.resource"
		if g.Type != nil && *g.Type == "Microsoft.Storage/storageAccounts" {
			typ = "azure.storage"
		}
		if acc, ok := extra.StorageGet[id]; ok {
			copyStorageAttrs(attrs, acc)
		}
		out = append(out, resource.Resource{
			Type: typ, UID: id, Account: sub,
			Aliases: []resource.Alias{
				{Kind: "cloud_arn", Value: id},
				{Kind: "azure_subscription_id", Value: sub},
			},
			Attrs: attrs,
		})
	}
	return out, nil
}

func copyStorageAttrs(attrs map[string]any, acc armstorage.Account) {
	if acc.Properties == nil || acc.Properties.Encryption == nil {
		return
	}
	enc := acc.Properties.Encryption
	if enc.Services != nil && enc.Services.Blob != nil && enc.Services.Blob.Enabled != nil {
		attrs["encryption_at_rest"] = *enc.Services.Blob.Enabled
	}
}

func subFromID(id string) string {
	const p = "/subscriptions/"
	if len(id) < len(p) || id[:len(p)] != p {
		return ""
	}
	rest := id[len(p):]
	for i := 0; i < len(rest); i++ {
		if rest[i] == '/' {
			return rest[:i]
		}
	}
	return rest
}
