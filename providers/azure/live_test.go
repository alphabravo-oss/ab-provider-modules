package azure

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
)

type recordedCredential struct{ calls atomic.Int32 }

func (credential *recordedCredential) GetToken(context.Context, policy.TokenRequestOptions) (azcore.AccessToken, error) {
	call := credential.calls.Add(1)
	token := azcore.AccessToken{Token: "recorded-token-2", ExpiresOn: time.Now().Add(time.Hour)}
	if call == 1 {
		token.Token = "recorded-token-1"
		token.ExpiresOn = time.Now().Add(-time.Minute)
	}
	return token, nil
}

func TestDiscoverLivePaginatesAndPreservesStoragePermissionWarning(t *testing.T) {
	var resourceAttempts atomic.Int32
	var sawRefreshedToken atomic.Bool
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer recorded-token-2" {
			sawRefreshedToken.Store(true)
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer recorded-token-") {
			t.Errorf("missing SDK bearer token: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/resources"):
			if resourceAttempts.Add(1) == 1 {
				w.Header().Set("Retry-After", "0")
				w.Header().Set("x-ms-retry-after-ms", "0")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":{"code":"TooManyRequests","message":"retry"}}`))
				return
			}
			_, _ = fmt.Fprintf(w, `{"value":[{"id":"/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm-1","name":"vm-1","type":"Microsoft.Compute/virtualMachines","location":"eastus"}],"nextLink":%q}`, server.URL+"/recorded/page-2")
		case r.URL.Path == "/recorded/page-2":
			_, _ = w.Write([]byte(`{"value":[{"id":"/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/logs","name":"logs","type":"Microsoft.Storage/storageAccounts","location":"westus"}]}`))
		case strings.Contains(r.URL.Path, "/storageAccounts/logs"):
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"code":"AuthorizationFailed","message":"storage properties denied"}}`))
		default:
			t.Errorf("unexpected Azure SDK request %s", r.URL.String())
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cloudConfig := cloud.Configuration{Services: map[cloud.ServiceName]cloud.ServiceConfiguration{
		cloud.ResourceManager: {Endpoint: server.URL, Audience: "https://management.azure.com/"},
	}}
	credential := &recordedCredential{}
	resources, warnings, err := DiscoverLive(context.Background(), LiveOpts{
		SubscriptionIDs: []string{"sub-1"}, Credential: credential,
		ClientOptions: &arm.ClientOptions{ClientOptions: azcore.ClientOptions{Cloud: cloudConfig, Transport: server.Client()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 3 {
		t.Fatalf("want account and two paginated resources, got %+v warnings=%v", resources, warnings)
	}
	if resourceAttempts.Load() != 2 || credential.calls.Load() < 2 || !sawRefreshedToken.Load() {
		t.Fatalf("retry/token refresh missing: resource attempts=%d token calls=%d refreshed=%v", resourceAttempts.Load(), credential.calls.Load(), sawRefreshedToken.Load())
	}
	if resources[1].Type != "azure.compute.instance" || resources[2].Type != "azure.storage" {
		t.Fatalf("provider mappings missing: %+v", resources)
	}
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "storage properties denied") || !strings.Contains(joined, "remains incomplete") {
		t.Fatalf("permission/preview warnings missing: %v", warnings)
	}
}
