package protocol

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/alphabravo-oss/ab-provider-modules/pkg/resource"
)

const APIVersion = "ab.provider/v1"

type Request struct {
	APIVersion string          `json:"apiVersion"`
	RequestID  string          `json:"requestId"`
	Operation  string          `json:"operation"`
	Provider   string          `json:"provider"`
	Config     json.RawMessage `json:"config,omitempty"`
}

type Error struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

type Response struct {
	APIVersion    string              `json:"apiVersion"`
	RequestID     string              `json:"requestId"`
	Provider      string              `json:"provider"`
	Version       string              `json:"version"`
	Status        string              `json:"status"`
	Resources     []resource.Resource `json:"resources,omitempty"`
	Warnings      []string            `json:"warnings,omitempty"`
	ServiceErrors []string            `json:"serviceErrors,omitempty"`
	Error         *Error              `json:"error,omitempty"`
}

type DiscoverFunc func(context.Context, json.RawMessage) ([]resource.Resource, []string, error)

// Serve executes the bounded provider protocol on stdin/stdout. Provider
// commands never accept fixture inventory or alternate wire formats.
func Serve(provider, version string, discover DiscoverFunc) {
	raw, readErr := io.ReadAll(io.LimitReader(os.Stdin, (64<<20)+1))
	if readErr != nil || len(raw) > 64<<20 {
		write(Response{APIVersion: APIVersion, Provider: provider, Version: version, Status: "failed", Error: &Error{Code: "invalid_request", Message: "protocol request exceeds 67108864 bytes"}})
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var request Request
	if err := decoder.Decode(&request); err != nil {
		write(Response{APIVersion: APIVersion, Provider: provider, Version: version, Status: "failed", Error: &Error{Code: "invalid_request", Message: "invalid bounded protocol request"}})
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		write(Response{APIVersion: APIVersion, RequestID: request.RequestID, Provider: provider, Version: version, Status: "failed", Error: &Error{Code: "invalid_request", Message: "exactly one protocol request is required"}})
		return
	}
	write(Handle(context.Background(), provider, version, request, discover))
}

func Handle(ctx context.Context, provider, version string, request Request, discover DiscoverFunc) Response {
	response := Response{APIVersion: APIVersion, RequestID: request.RequestID, Provider: provider, Version: version, Status: "complete"}
	if request.APIVersion != APIVersion || request.RequestID == "" || request.Provider != provider || (request.Operation != "discover" && request.Operation != "health") {
		response.Status = "failed"
		response.Error = &Error{Code: "incompatible_protocol", Message: fmt.Sprintf("%s discover or health request required", APIVersion)}
		return response
	}
	if discover == nil {
		response.Status = "failed"
		response.Error = &Error{Code: "discovery_failed", Message: "provider discovery handler is unavailable"}
		return response
	}
	resources, serviceErrors, err := discover(ctx, request.Config)
	response.Resources, response.ServiceErrors = resources, serviceErrors
	if err != nil {
		response.Status = "failed"
		response.Error = &Error{Code: "discovery_failed", Message: safeError(err, request.Config), Retryable: true}
	} else if len(serviceErrors) > 0 {
		response.Status = "partial"
	}
	return response
}

func safeError(err error, config json.RawMessage) string {
	if err == nil {
		return ""
	}
	// Provider errors can include request identifiers, but config values are
	// never echoed. The control plane applies its own redaction as a second line.
	text := err.Error()
	var values map[string]any
	if json.Unmarshal(config, &values) == nil {
		redactSensitiveValues(&text, values)
	}
	if len(text) > 2048 {
		text = text[:2048]
	}
	return text
}

func redactSensitiveValues(text *string, values map[string]any) {
	for key, raw := range values {
		switch value := raw.(type) {
		case map[string]any:
			redactSensitiveValues(text, value)
		case []any:
			for _, item := range value {
				if nested, ok := item.(map[string]any); ok {
					redactSensitiveValues(text, nested)
				}
			}
		case string:
			lower := strings.ToLower(key)
			if value != "" && (strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "password") || strings.Contains(lower, "passphrase") || strings.Contains(lower, "privatekey") || strings.Contains(lower, "accesskey")) {
				*text = strings.ReplaceAll(*text, value, "[REDACTED]")
			}
		}
	}
}

func write(response Response) {
	_ = json.NewEncoder(os.Stdout).Encode(response)
}
