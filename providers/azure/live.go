package azure

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage"

	"github.com/alphabravo-oss/ab-provider-modules/pkg/resource"
)

type LiveOpts struct {
	TenantID, ClientID, ClientSecret string
	SubscriptionIDs                  []string
	Credential                       azcore.TokenCredential
	ClientOptions                    *arm.ClientOptions
}

func DiscoverLive(ctx context.Context, options LiveOpts) ([]resource.Resource, []string, error) {
	if len(options.SubscriptionIDs) == 0 {
		return nil, nil, fmt.Errorf("Azure subscriptionIds required")
	}
	credential := options.Credential
	if credential == nil {
		if options.TenantID == "" || options.ClientID == "" || options.ClientSecret == "" {
			return nil, nil, fmt.Errorf("Azure tenantId, clientId, and clientSecret required")
		}
		created, err := azidentity.NewClientSecretCredential(options.TenantID, options.ClientID, options.ClientSecret, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("Azure credentials: %w", err)
		}
		credential = created
	}
	var out []resource.Resource
	var serviceErrors []string
	for _, subscription := range options.SubscriptionIDs {
		subscription = strings.TrimSpace(subscription)
		if subscription == "" {
			continue
		}
		out = append(out, resource.Resource{Type: "cloud_account", UID: subscription, Account: subscription, Aliases: []resource.Alias{{Kind: "azure_subscription_id", Value: subscription}}})
		resourcesClient, err := armresources.NewClient(subscription, credential, options.ClientOptions)
		if err != nil {
			serviceErrors = append(serviceErrors, "subscription "+subscription+" resources client: "+err.Error())
			continue
		}
		storageClient, storageErr := armstorage.NewAccountsClient(subscription, credential, options.ClientOptions)
		if storageErr != nil {
			serviceErrors = append(serviceErrors, "subscription "+subscription+" storage client: "+storageErr.Error())
		}
		pager := resourcesClient.NewListPager(nil)
		for pager.More() {
			page, pageErr := pager.NextPage(ctx)
			if pageErr != nil {
				serviceErrors = append(serviceErrors, "subscription "+subscription+" resources: "+pageErr.Error())
				break
			}
			for _, item := range page.Value {
				if item == nil || item.ID == nil || *item.ID == "" {
					continue
				}
				uid := *item.ID
				nativeType := azureResourceType(value(item.Type))
				attrs := map[string]any{"name": value(item.Name), "location": value(item.Location), "azure_type": value(item.Type), "tags": stringMap(item.Tags)}
				if nativeType == "azure.storage" && storageClient != nil {
					group, name := azureResourceGroupAndName(uid)
					if group == "" || name == "" {
						serviceErrors = append(serviceErrors, "storage resource has unparseable ID: "+uid)
					} else if properties, getErr := storageClient.GetProperties(ctx, group, name, nil); getErr != nil {
						serviceErrors = append(serviceErrors, "storage "+uid+": "+getErr.Error())
					} else {
						copyStorageAttrs(attrs, properties.Account)
						if properties.Properties != nil {
							attrs["allow_blob_public_access"] = properties.Properties.AllowBlobPublicAccess
							attrs["allow_shared_key_access"] = properties.Properties.AllowSharedKeyAccess
							attrs["https_only"] = properties.Properties.EnableHTTPSTrafficOnly
							attrs["minimum_tls_version"] = properties.Properties.MinimumTLSVersion
							attrs["public_network_access"] = properties.Properties.PublicNetworkAccess
							attrs["network_rule_set"] = properties.Properties.NetworkRuleSet
						}
					}
				}
				out = append(out, resource.Resource{Type: nativeType, UID: uid, Account: subscription, Region: value(item.Location),
					Aliases: []resource.Alias{{Kind: "azure_resource_id", Value: uid}}, Attrs: attrs,
					Edges: []resource.Edge{{From: subscription, To: uid, Kind: "contains"}},
				})
			}
		}
	}
	if len(out) == 0 {
		return nil, serviceErrors, fmt.Errorf("Azure discovery produced no subscription inventory")
	}
	serviceErrors = append(serviceErrors, "Azure preview enriches Storage accounts; service-specific security context for other resource types remains incomplete")
	return out, serviceErrors, nil
}

func azureResourceType(value string) string {
	switch strings.ToLower(value) {
	case "microsoft.storage/storageaccounts":
		return "azure.storage"
	case "microsoft.compute/virtualmachines":
		return "azure.compute.instance"
	case "microsoft.network/networksecuritygroups":
		return "azure.network_security_group"
	case "microsoft.sql/servers/databases":
		return "azure.sql.database"
	default:
		return "azure.resource"
	}
}

func azureResourceGroupAndName(id string) (string, string) {
	parts := strings.Split(strings.Trim(id, "/"), "/")
	group, name := "", ""
	for index := 0; index+1 < len(parts); index++ {
		if strings.EqualFold(parts[index], "resourceGroups") {
			group = parts[index+1]
		}
	}
	if len(parts) > 0 {
		name = parts[len(parts)-1]
	}
	return group, name
}

func value(pointer *string) string {
	if pointer == nil {
		return ""
	}
	return *pointer
}

func stringMap(input map[string]*string) map[string]string {
	if input == nil {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = valueString(value)
	}
	return out
}

func valueString(pointer *string) string { return value(pointer) }
