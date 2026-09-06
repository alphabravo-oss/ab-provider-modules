package hetzner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

func TestDiscoverLiveRetriesAndPaginatesServers(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if attempts.Add(1) == 1 {
			response.Header().Set("Retry-After", "0")
			response.WriteHeader(http.StatusTooManyRequests)
			response.Write([]byte(`{"error":{"code":"rate_limit_exceeded","message":"retry"}}`))
			return
		}
		if request.URL.Query().Get("page") == "2" {
			response.Write([]byte(`{"servers":[{"id":2,"name":"worker","status":"running","public_net":{"ipv4":{"ip":"203.0.113.2"}},"location":{"name":"fsn1"}}],"meta":{"pagination":{"page":2,"last_page":2,"per_page":25,"total_entries":2}}}`))
			return
		}
		response.Write([]byte(`{"servers":[{"id":1,"name":"web","status":"running","backup_window":"22-02","rescue_enabled":false,"public_net":{"ipv4":{"ip":"203.0.113.1"}},"location":{"name":"fsn1"}}],"meta":{"pagination":{"page":1,"last_page":2,"next_page":2,"per_page":25,"total_entries":2}}}`))
	}))
	defer server.Close()
	client := hcloud.NewClient(hcloud.WithToken("token"), hcloud.WithEndpoint(server.URL), hcloud.WithHTTPClient(server.Client()), hcloud.WithRetryOpts(hcloud.RetryOpts{MaxRetries: 2, BackoffFunc: hcloud.ConstantBackoff(0 * time.Second)}))
	resources, warnings, err := DiscoverLive(context.Background(), LiveOpts{ProjectID: "project-1", Client: client})
	if err != nil || len(warnings) != 1 || len(resources) != 3 || resources[1].Attrs["publicly_exposed"] != true || attempts.Load() != 3 {
		t.Fatalf("discover: err=%v warnings=%v resources=%+v", err, warnings, resources)
	}
}

func TestDiscoverLivePreservesProjectOnServerPermissionFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusForbidden)
		response.Write([]byte(`{"error":{"code":"forbidden","message":"server list denied"}}`))
	}))
	defer server.Close()
	client := hcloud.NewClient(hcloud.WithToken("token"), hcloud.WithEndpoint(server.URL), hcloud.WithHTTPClient(server.Client()))
	resources, warnings, err := DiscoverLive(context.Background(), LiveOpts{ProjectID: "project-1", Client: client})
	if err != nil || len(resources) != 1 || !strings.Contains(strings.Join(warnings, "\n"), "server list denied") {
		t.Fatalf("permission failure must preserve project and partial truth: resources=%+v warnings=%v err=%v", resources, warnings, err)
	}
}

func TestListServersRejectsIncompleteInventory(t *testing.T) {
	for _, tc := range []struct {
		name, last    string
		status, limit int
	}{
		{"later page fails", `{"error":{"code":"forbidden","message":"denied"}}`, 403, 10},
		{"missing pagination", `{"servers":[]}`, 200, 10},
		{"repeated page", `{"servers":[],"meta":{"pagination":{"next_page":2}}}`, 200, 10},
		{"resource bound", `{"servers":[{"id":2}],"meta":{"pagination":{"next_page":0}}}`, 200, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Query().Get("page") == "1" {
					w.Write([]byte(`{"servers":[{"id":1}],"meta":{"pagination":{"next_page":2}}}`))
					return
				}
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.last))
			}))
			defer server.Close()
			client := hcloud.NewClient(hcloud.WithToken("test-token"), hcloud.WithEndpoint(server.URL), hcloud.WithHTTPClient(server.Client()))
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			rows, err := ListServers(ctx, client, tc.limit)
			if err == nil || len(rows) != 0 {
				t.Fatalf("incomplete inventory escaped: rows=%v err=%v", rows, err)
			}
		})
	}
}
