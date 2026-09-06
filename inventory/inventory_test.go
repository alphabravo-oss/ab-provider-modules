package inventory

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCollectCompleteOrNothing(t *testing.T) {
	for _, tc := range []struct{ cloud, host, first, last string }{
		{"aws", "ec2.us-east-1.amazonaws.com", `<DescribeInstancesResponse><nextToken>next</nextToken><reservationSet><item><instancesSet><item><instanceId>i-123</instanceId><instanceState><name>running</name></instanceState><instanceType>t3.micro</instanceType></item></instancesSet></item></reservationSet></DescribeInstancesResponse>`, `<DescribeInstancesResponse><reservationSet/></DescribeInstancesResponse>`},
		{"digitalocean", "api.digitalocean.com", `{"droplets":[{"id":123,"name":"web","region":{"slug":"nyc3"},"status":"active"}],"links":{"pages":{"next":"https://api.digitalocean.com/v2/droplets?page=2","last":"https://api.digitalocean.com/v2/droplets?page=2"}}}`, `{"droplets":[],"links":{}}`},
		{"hetzner", "api.hetzner.cloud", `{"servers":[{"id":123,"name":"web","location":{"name":"fsn1"},"status":"running"}],"meta":{"pagination":{"next_page":2}}}`, `{"servers":[],"meta":{"pagination":{"next_page":0}}}`},
	} {
		t.Run(tc.cloud, func(t *testing.T) {
			for _, fail := range []bool{false, true} {
				calls := 0
				client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
					calls++
					if calls > 2 || r.URL.Host != tc.host || r.Header.Get("Authorization") == "" {
						t.Fatalf("unexpected request: %s", r.URL)
					}
					body, status := tc.first, 200
					if calls == 2 {
						body = tc.last
						if fail {
							status = 403
							body = "denied"
						}
					}
					content := "application/json"
					if tc.cloud == "aws" {
						content = "text/xml"
					}
					return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {content}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
				})}
				req := Request{Provider: tc.cloud, Credential: "test-token", AWS: aws.Credentials{AccessKeyID: "test", SecretAccessKey: "test"}}
				if tc.cloud == "aws" {
					req.Region = "us-east-1"
				}
				rows, err := Collect(context.Background(), req, client, []string{"compute.server"})
				if calls != 2 {
					t.Fatalf("pagination calls: %d", calls)
				}
				if fail {
					if err == nil || len(rows) != 0 {
						t.Fatalf("partial inventory escaped: %v %v", rows, err)
					}
				} else if err != nil || len(rows) != 1 || rows[0].Kind != "compute.server" || rows[0].NativeID == "" {
					t.Fatalf("complete inventory lost: %v %v", rows, err)
				}
			}
		})
	}
}

func TestCollectRejectsUnsupportedConfiguration(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		calls++
		t.Fatal("invalid request reached provider")
		return nil, nil
	})}
	for _, req := range []Request{{Provider: "unknown"}, {Provider: "aws", Region: "us-east-1"}, {Provider: "hetzner"}, {Provider: "digitalocean", Credential: "token", Region: "../bad"}} {
		if _, err := Collect(context.Background(), req, client, []string{"compute.server"}); err == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
	if calls != 0 {
		t.Fatal("unexpected provider call")
	}
}

func TestImageCatalogOwnership(t *testing.T) {
	for _, tc := range []struct{ cloud, body string }{
		{"digitalocean", `{"images":[{"id":1,"name":"public","public":true,"status":"available"},{"id":2,"name":"owned","public":false,"status":"available"}],"links":{}}`},
		{"hetzner", `{"images":[{"id":1,"name":"public","type":"system","status":"available"},{"id":2,"description":"owned","type":"snapshot","status":"available"}],"meta":{"pagination":{}}}`},
	} {
		t.Run(tc.cloud, func(t *testing.T) {
			client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(tc.body)), Request: r}, nil
			})}
			rows, err := Collect(context.Background(), Request{Provider: tc.cloud, Credential: "test-token"}, client, []string{"compute.image"})
			if err != nil || len(rows) != 2 || !rows[0].Catalog || rows[1].Catalog {
				t.Fatalf("image ownership classification: %v %v", rows, err)
			}
		})
	}
}
