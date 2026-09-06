package oci

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/core"
)

type recordedSigner struct{ calls atomic.Int32 }

func (signer *recordedSigner) Sign(request *http.Request) error {
	request.Header.Set("Authorization", "Signature recorded-"+fmt.Sprint(signer.calls.Add(1)))
	return nil
}

func recordedComputeClient(server *httptest.Server) *core.ComputeClient {
	return &core.ComputeClient{BaseClient: common.BaseClient{
		HTTPClient: server.Client(), Signer: &recordedSigner{}, Host: server.URL, BasePath: "20160918", UserAgent: "ab-provider-recorded-test",
	}}
}

func TestDiscoverLivePaginatesOCICompute(t *testing.T) {
	requests := 0
	throttled := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Signature recorded-") || r.URL.Path != "/20160918/instances" {
			t.Errorf("unexpected OCI request path=%q auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		if !throttled {
			throttled = true
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"code":"TooManyRequests","message":"throttled"}`))
			return
		}
		if r.URL.Query().Get("page") == "page-2" {
			_, _ = w.Write([]byte(`[{"id":"ocid1.instance.oc1..two","displayName":"two","region":"us-phoenix-1","lifecycleState":"RUNNING"}]`))
			return
		}
		w.Header().Set("opc-next-page", "page-2")
		_, _ = w.Write([]byte(`[{"id":"ocid1.instance.oc1..one","displayName":"one","region":"us-phoenix-1","lifecycleState":"RUNNING"}]`))
	}))
	defer server.Close()
	resources, warnings, err := DiscoverLive(context.Background(), LiveOpts{TenancyOCID: "ocid1.tenancy.oc1..root", Region: "us-phoenix-1", ComputeClient: recordedComputeClient(server)})
	if err != nil || len(resources) != 3 || requests != 3 {
		t.Fatalf("resources=%+v warnings=%v requests=%d err=%v", resources, warnings, requests, err)
	}
	if resources[1].UID != "ocid1.instance.oc1..one" || resources[2].UID != "ocid1.instance.oc1..two" {
		t.Fatalf("OCI page results missing: %+v", resources)
	}
}

func TestDiscoverLiveReturnsPermissionFailureAsPartialServiceError(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":"NotAuthorizedOrNotFound","message":"compute list denied"}`))
	}))
	defer server.Close()
	resources, warnings, err := DiscoverLive(context.Background(), LiveOpts{TenancyOCID: "ocid1.tenancy.oc1..root", ComputeClient: recordedComputeClient(server)})
	if err != nil || len(resources) != 1 || len(warnings) != 1 || !strings.Contains(warnings[0], "compute list denied") {
		t.Fatalf("permission failure must preserve account and partial truth: resources=%+v warnings=%v err=%v", resources, warnings, err)
	}
}
