package linode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/linode/linodego"
)

func TestDiscoverLiveRetriesAndPaginatesInstances(t *testing.T) {
	var accountAttempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v4/account":
			if accountAttempts.Add(1) == 1 {
				response.Header().Set("Retry-After", "0")
				response.WriteHeader(http.StatusTooManyRequests)
				response.Write([]byte(`{"errors":[{"reason":"retry"}]}`))
				return
			}
			response.Write([]byte(`{"euuid":"account-1","capabilities":["Linodes"]}`))
		case "/v4/linode/instances":
			if request.URL.Query().Get("page") == "2" {
				response.Write([]byte(`{"data":[{"id":124,"label":"worker","region":"us-east","status":"running","disk_encryption":"enabled","ipv4":[]}],"page":2,"pages":2,"results":2}`))
				return
			}
			response.Write([]byte(`{"data":[{"id":123,"label":"web","region":"us-east","status":"running","disk_encryption":"enabled","ipv4":["203.0.113.10"],"backups":{"enabled":true}}],"page":1,"pages":2,"results":2}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	client := linodego.NewClient(server.Client())
	client.SetBaseURL(server.URL)
	client.SetRetryCount(2).SetRetryWaitTime(0 * time.Second).SetRetryMaxWaitTime(time.Millisecond)
	resources, warnings, err := DiscoverLive(context.Background(), LiveOpts{Client: &client})
	if err != nil || len(warnings) != 1 || len(resources) != 3 || resources[1].Attrs["encryption_at_rest"] != true || accountAttempts.Load() != 2 {
		t.Fatalf("discover: err=%v warnings=%v resources=%+v", err, warnings, resources)
	}
}

func TestDiscoverLivePreservesAccountOnInstancePermissionFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/v4/account" {
			response.Write([]byte(`{"euuid":"account-1","capabilities":["Linodes"]}`))
			return
		}
		response.WriteHeader(http.StatusForbidden)
		response.Write([]byte(`{"errors":[{"reason":"instance list denied"}]}`))
	}))
	defer server.Close()
	client := linodego.NewClient(server.Client())
	client.SetBaseURL(server.URL)
	resources, warnings, err := DiscoverLive(context.Background(), LiveOpts{Client: &client})
	if err != nil || len(resources) != 1 || !strings.Contains(strings.Join(warnings, "\n"), "instance list denied") {
		t.Fatalf("permission failure must preserve account and partial truth: resources=%+v warnings=%v err=%v", resources, warnings, err)
	}
}
