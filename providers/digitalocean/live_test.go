package digitalocean

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/digitalocean/godo"
	"golang.org/x/oauth2"
)

func TestDiscoverLiveRetriesAndPaginatesDroplets(t *testing.T) {
	var accountAttempts atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v2/account":
			if accountAttempts.Add(1) == 1 {
				response.Header().Set("Retry-After", "0")
				response.WriteHeader(http.StatusTooManyRequests)
				response.Write([]byte(`{"id":"too_many_requests","message":"retry"}`))
				return
			}
			response.Write([]byte(`{"account":{"uuid":"account-1","status":"active","email_verified":true}}`))
		case "/v2/droplets":
			if request.URL.Query().Get("page") == "2" {
				response.Write([]byte(`{"droplets":[{"id":10,"name":"worker","status":"active","region":{"slug":"nyc3"}}],"links":{"pages":{}},"meta":{"total":2}}`))
				return
			}
			response.Write([]byte(`{"droplets":[{"id":9,"name":"web","status":"active","region":{"slug":"nyc3"},"networks":{"v4":[{"ip_address":"203.0.113.9","type":"public"}]},"backup_ids":[1]}],"links":{"pages":{"next":"` + server.URL + `/v2/droplets?page=2","last":"` + server.URL + `/v2/droplets?page=2"}},"meta":{"total":2}}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	zero := 0.0
	authContext := context.WithValue(context.Background(), oauth2.HTTPClient, server.Client())
	authClient := oauth2.NewClient(authContext, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "token"}))
	client, err := godo.New(authClient, godo.WithRetryAndBackoffs(godo.RetryConfig{RetryMax: 2, RetryWaitMin: &zero, RetryWaitMax: &zero}))
	if err != nil {
		t.Fatal(err)
	}
	client.BaseURL, _ = url.Parse(server.URL + "/")
	resources, warnings, err := DiscoverLive(context.Background(), LiveOpts{Client: client})
	if err != nil || len(warnings) != 1 || len(resources) != 3 || resources[1].Attrs["publicly_exposed"] != true || accountAttempts.Load() != 2 {
		t.Fatalf("discover: err=%v warnings=%v resources=%+v", err, warnings, resources)
	}
}

func TestDiscoverLivePreservesAccountOnDropletPermissionFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/v2/account" {
			response.Write([]byte(`{"account":{"uuid":"account-1","status":"active"}}`))
			return
		}
		response.WriteHeader(http.StatusForbidden)
		response.Write([]byte(`{"id":"forbidden","message":"droplet list denied"}`))
	}))
	defer server.Close()
	client := godo.NewClient(server.Client())
	client.BaseURL, _ = url.Parse(server.URL + "/")
	resources, warnings, err := DiscoverLive(context.Background(), LiveOpts{Client: client})
	if err != nil || len(resources) != 1 || !strings.Contains(strings.Join(warnings, "\n"), "droplet list denied") {
		t.Fatalf("permission failure must preserve account and partial truth: resources=%+v warnings=%v err=%v", resources, warnings, err)
	}
}
