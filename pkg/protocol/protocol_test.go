package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/alphabravo-oss/ab-provider-modules/pkg/resource"
)

func TestHandleIdentityPartialAndErrorRedaction(t *testing.T) {
	valid := Request{APIVersion: APIVersion, RequestID: "request-1", Provider: "test", Operation: "discover", Config: json.RawMessage(`{"clientSecret":"canary-secret","nested":{"privateKey":"canary-key"}}`)}
	response := Handle(context.Background(), "test", "2.0.0", valid, func(context.Context, json.RawMessage) ([]resource.Resource, []string, error) {
		return []resource.Resource{{UID: "a", Type: "cloud_account"}}, []string{"service not authorized"}, nil
	})
	if response.Status != "partial" || response.RequestID != "request-1" || len(response.Resources) != 1 {
		t.Fatalf("partial response: %+v", response)
	}
	response = Handle(context.Background(), "test", "2.0.0", valid, func(context.Context, json.RawMessage) ([]resource.Resource, []string, error) {
		return nil, nil, errors.New("credential canary-secret key canary-key rejected")
	})
	encoded, _ := json.Marshal(response)
	if response.Status != "failed" || response.Error == nil || strings.Contains(string(encoded), "canary-secret") || strings.Contains(string(encoded), "canary-key") || !strings.Contains(response.Error.Message, "[REDACTED]") {
		t.Fatalf("error was not safely redacted: %s", encoded)
	}
}

func TestHandleRejectsProtocolMismatchWithoutCallingProvider(t *testing.T) {
	called := false
	response := Handle(context.Background(), "aws", "2.0.0", Request{APIVersion: "wrong", RequestID: "r", Provider: "aws", Operation: "discover"}, func(context.Context, json.RawMessage) ([]resource.Resource, []string, error) {
		called = true
		return nil, nil, nil
	})
	if response.Status != "failed" || response.Error == nil || called {
		t.Fatalf("mismatch response=%+v called=%v", response, called)
	}
}
