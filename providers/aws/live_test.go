package aws

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
)

type rotatingCredentials struct{ calls atomic.Int32 }

func (provider *rotatingCredentials) Retrieve(context.Context) (awssdk.Credentials, error) {
	call := provider.calls.Add(1)
	credentials := awssdk.Credentials{AccessKeyID: "SECOND", SecretAccessKey: "second-secret", Source: "recorded", CanExpire: true, Expires: time.Now().Add(time.Hour)}
	if call == 1 {
		credentials.AccessKeyID = "FIRST"
		credentials.SecretAccessKey = "first-secret"
		credentials.Expires = time.Now().Add(-time.Minute)
	}
	return credentials, nil
}

func TestDiscoverLivePaginatesRetriesRefreshesCredentialsAndPreservesIAMPermissionWarning(t *testing.T) {
	var listAttempts atomic.Int32
	var sawRefreshedCredential atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Authorization"), "Credential=SECOND/") {
			sawRefreshedCredential.Store(true)
		}
		body, _ := io.ReadAll(r.Body)
		s := r.URL.String() + " " + string(body) + " " + r.Header.Get("X-Amz-Target")
		w.Header().Set("Content-Type", "application/xml")
		switch {
		case r.Method == "GET" && !strings.Contains(r.URL.RawQuery, "publicAccessBlock"):
			if listAttempts.Add(1) == 1 {
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(http.StatusServiceUnavailable)
				w.Write([]byte(`<Error><Code>SlowDown</Code><Message>throttled</Message></Error>`))
				return
			}
			if r.URL.Query().Get("continuation-token") == "page-2" {
				w.Write([]byte(`<?xml version="1.0"?><ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Owner><ID>123456789012</ID></Owner><Buckets><Bucket><Name>archive</Name><CreationDate>2020-01-01T00:00:00.000Z</CreationDate></Bucket></Buckets></ListAllMyBucketsResult>`))
				return
			}
			w.Write([]byte(`<?xml version="1.0"?><ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Owner><ID>123456789012</ID><DisplayName>o</DisplayName></Owner><Buckets><Bucket><Name>payments</Name><CreationDate>2020-01-01T00:00:00.000Z</CreationDate></Bucket></Buckets><ContinuationToken>page-2</ContinuationToken></ListAllMyBucketsResult>`))
		case strings.Contains(s, "publicAccessBlock") || strings.Contains(r.URL.Path, "publicAccessBlock"):
			w.Write([]byte(`<?xml version="1.0"?><PublicAccessBlockConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><IgnorePublicAcls>false</IgnorePublicAcls><RestrictPublicBuckets>false</RestrictPublicBuckets><BlockPublicAcls>false</BlockPublicAcls><BlockPublicPolicy>false</BlockPublicPolicy></PublicAccessBlockConfiguration>`))
		case strings.Contains(string(body), "GetAccountSummary") || strings.Contains(s, "GetAccountSummary"):
			w.Header().Set("Content-Type", "text/xml")
			w.Write([]byte(`<GetAccountSummaryResponse><GetAccountSummaryResult><SummaryMap><entry><key>AccountMFAEnabled</key><value>0</value></entry></SummaryMap></GetAccountSummaryResult></GetAccountSummaryResponse>`))
		case strings.Contains(string(body), "GetAccountPasswordPolicy"):
			w.Header().Set("Content-Type", "text/xml")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`<ErrorResponse><Error><Code>AccessDenied</Code><Message>password policy denied</Message></Error></ErrorResponse>`))
		default:
			w.WriteHeader(200)
			w.Write([]byte(`<ok/>`))
		}
	}))
	defer srv.Close()
	credentials := &rotatingCredentials{}
	rs, warnings, err := DiscoverLive(context.Background(), LiveOpts{HTTPClient: srv.Client(), Endpoint: srv.URL, Region: "us-east-1", Credentials: credentials})
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{"payments": false, "archive": false}
	for _, r := range rs {
		for bucket := range found {
			if r.Type == "aws.s3_bucket" && strings.Contains(r.UID, bucket) {
				found[bucket] = true
			}
		}
	}
	if !found["payments"] || !found["archive"] || listAttempts.Load() < 3 {
		t.Fatalf("paginated/retried bucket discovery missing: resources=%+v attempts=%d", rs, listAttempts.Load())
	}
	if credentials.calls.Load() < 2 || !sawRefreshedCredential.Load() {
		t.Fatalf("expiring credentials were not refreshed: retrieves=%d refreshed_auth=%v", credentials.calls.Load(), sawRefreshedCredential.Load())
	}
	if !strings.Contains(strings.Join(warnings, "\n"), "password policy denied") {
		t.Fatalf("IAM permission warning missing: %v", warnings)
	}
}
