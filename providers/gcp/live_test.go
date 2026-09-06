package gcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

type rotatingTokenSource struct{ calls atomic.Int32 }

func (source *rotatingTokenSource) Token() (*oauth2.Token, error) {
	call := source.calls.Add(1)
	token := &oauth2.Token{AccessToken: "token-2", TokenType: "Bearer", Expiry: time.Now().Add(time.Hour)}
	if call == 1 {
		token.AccessToken = "token-1"
		token.Expiry = time.Now().Add(-time.Minute)
	}
	return token, nil
}

func TestDiscoverLiveRecordedPaginationThrottlePermissionAndTokenRefresh(t *testing.T) {
	var computeAttempts atomic.Int32
	var sawRefreshedToken atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") == "Bearer token-2" {
			sawRefreshedToken.Store(true)
		}
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/compute/projects/proj-1/aggregated/instances":
			if computeAttempts.Add(1) == 1 {
				response.Header().Set("Retry-After", "0")
				response.WriteHeader(http.StatusTooManyRequests)
				response.Write([]byte(`{"error":{"message":"rate limited"}}`))
				return
			}
			if request.URL.Query().Get("pageToken") == "compute-2" {
				response.Write([]byte(`{"items":{"zones/us-central1-b":{"instances":[{"id":"2","name":"worker","selfLink":"//compute.googleapis.com/projects/proj-1/zones/us-central1-b/instances/worker","zone":"zones/us-central1-b","status":"RUNNING"}]}}}`))
				return
			}
			response.Write([]byte(`{"items":{"zones/us-central1-a":{"instances":[{"id":"1","name":"web","selfLink":"//compute.googleapis.com/projects/proj-1/zones/us-central1-a/instances/web","zone":"zones/us-central1-a","status":"RUNNING","networkInterfaces":[{"accessConfigs":[{"natIP":"203.0.113.10"}]}],"shieldedInstanceConfig":{"enableSecureBoot":true}}]}},"nextPageToken":"compute-2"}`))
		case "/compute/projects/proj-1/global/firewalls":
			response.WriteHeader(http.StatusForbidden)
			response.Write([]byte(`{"error":{"message":"firewall list denied"}}`))
		case "/storage/b":
			if request.URL.Query().Get("pageToken") == "bucket-2" {
				response.Write([]byte(`{"items":[{"name":"archive","location":"US"}]}`))
				return
			}
			response.Write([]byte(`{"items":[{"name":"logs","location":"US","iamConfiguration":{"publicAccessPrevention":"enforced","uniformBucketLevelAccess":{"enabled":true}},"encryption":{"defaultKmsKeyName":"projects/p/keys/k"}}],"nextPageToken":"bucket-2"}`))
		case "/sql/projects/proj-1/instances":
			if request.URL.Query().Get("pageToken") == "sql-2" {
				response.Write([]byte(`{"items":[{"name":"analytics","selfLink":"//sqladmin.googleapis.com/projects/proj-1/instances/analytics","region":"us-central1"}]}`))
				return
			}
			response.Write([]byte(`{"items":[{"name":"db","selfLink":"//sqladmin.googleapis.com/projects/proj-1/instances/db","region":"us-central1","settings":{"ipConfiguration":{"ipv4Enabled":false,"requireSsl":true}}}],"nextPageToken":"sql-2"}`))
		case "/resource/projects/proj-1:getIamPolicy":
			response.Write([]byte(`{"version":3,"bindings":[{"role":"roles/viewer","members":["group:ops@example.test"]}]}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	tokens := &rotatingTokenSource{}
	resources, warnings, err := DiscoverLive(context.Background(), LiveOpts{
		ProjectIDs: []string{"proj-1"}, HTTPClient: server.Client(), TokenSource: tokens,
		ComputeEndpoint: server.URL + "/compute", StorageEndpoint: server.URL + "/storage",
		SQLEndpoint: server.URL + "/sql", ResourceEndpoint: server.URL + "/resource",
	})
	if err != nil || len(warnings) != 2 || len(resources) != 8 {
		t.Fatalf("discover: err=%v warnings=%v resources=%+v", err, warnings, resources)
	}
	if computeAttempts.Load() != 3 || tokens.calls.Load() < 2 || !sawRefreshedToken.Load() {
		t.Fatalf("retry/token refresh missing: attempts=%d token_calls=%d refreshed=%v", computeAttempts.Load(), tokens.calls.Load(), sawRefreshedToken.Load())
	}
	if !strings.Contains(strings.Join(warnings, "\n"), "firewall list denied") {
		t.Fatalf("permission warning missing: %v", warnings)
	}
	found := map[string]bool{"web": false, "worker": false, "logs": false, "archive": false, "db": false, "analytics": false}
	for _, item := range resources {
		if item.Type == "gcp.instance" && item.Attrs["publicly_exposed"] != true {
			if item.Attrs["name"] == "web" {
				t.Fatalf("instance exposure lost: %+v", item)
			}
		}
		if item.Type == "gcp.bucket" && item.Attrs["public_access_prevention"] != "enforced" {
			if item.UID == "gs://logs" {
				t.Fatalf("bucket context lost: %+v", item)
			}
		}
		for name := range found {
			if strings.Contains(item.UID, name) {
				found[name] = true
			}
		}
	}
	for name, present := range found {
		if !present {
			t.Errorf("paginated resource %q missing: %+v", name, resources)
		}
	}
}
