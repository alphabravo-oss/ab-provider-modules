package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/alphabravo-oss/ab-provider-modules/pkg/protocol"
	awsprov "github.com/alphabravo-oss/ab-provider-modules/providers/aws"
)

const providerVersion = "2.0.0"

func main() {
	raw, readErr := io.ReadAll(io.LimitReader(os.Stdin, (64<<20)+1))
	var request protocol.Request
	if readErr != nil || len(raw) > 64<<20 || json.Unmarshal(raw, &request) != nil {
		write(protocol.Response{APIVersion: protocol.APIVersion, Provider: "aws", Version: providerVersion, Status: "failed", Error: &protocol.Error{Code: "invalid_request", Message: "invalid bounded protocol request"}})
		return
	}
	response := protocol.Response{
		APIVersion: protocol.APIVersion, RequestID: request.RequestID,
		Provider: "aws", Version: providerVersion,
	}
	if request.APIVersion != protocol.APIVersion || request.RequestID == "" || request.Provider != "aws" || (request.Operation != "discover" && request.Operation != "health") {
		response.Status = "failed"
		response.Error = &protocol.Error{Code: "incompatible_protocol", Message: "ab.provider/v1 discover or health request required"}
		write(response)
		return
	}
	options, err := liveOptions(request.Config)
	if err != nil {
		response.Status = "failed"
		response.Error = &protocol.Error{Code: "invalid_config", Message: err.Error()}
		write(response)
		return
	}
	resources, serviceErrors, err := awsprov.DiscoverLive(context.Background(), options)
	response.Resources = resources
	response.ServiceErrors = serviceErrors
	if err != nil {
		response.Status = "failed"
		response.Error = &protocol.Error{Code: "discovery_failed", Message: err.Error(), Retryable: true}
	} else if len(serviceErrors) > 0 {
		response.Status = "partial"
		response.Warnings = []string{fmt.Sprintf("%d AWS service calls failed", len(serviceErrors))}
	} else {
		response.Status = "complete"
	}
	write(response)
}

func liveOptions(raw json.RawMessage) (awsprov.LiveOpts, error) {
	var config struct {
		AccessKeyID     string `json:"accessKeyId"`
		SecretAccessKey string `json:"secretAccessKey"`
		SessionToken    string `json:"sessionToken"`
		Region          string `json:"region"`
		Endpoint        string `json:"endpoint"`
		Profile         string `json:"profile"`
		RoleARN         string `json:"roleArn"`
		ExternalID      string `json:"externalId"`
		RoleSessionName string `json:"roleSessionName"`
	}
	if len(raw) > 0 && json.Unmarshal(raw, &config) != nil {
		return awsprov.LiveOpts{}, fmt.Errorf("configuration must be a JSON object")
	}
	if (config.AccessKeyID == "") != (config.SecretAccessKey == "") {
		return awsprov.LiveOpts{}, fmt.Errorf("accessKeyId and secretAccessKey must be supplied together")
	}
	return awsprov.LiveOpts{
		AccessKey: config.AccessKeyID, Secret: config.SecretAccessKey, Session: config.SessionToken,
		Region: config.Region, Endpoint: config.Endpoint, Profile: config.Profile,
		RoleARN: config.RoleARN, ExternalID: config.ExternalID, RoleSessionName: config.RoleSessionName,
	}, nil
}

func write(response protocol.Response) {
	_ = json.NewEncoder(os.Stdout).Encode(response)
}
