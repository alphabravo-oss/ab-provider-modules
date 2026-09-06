package gcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/alphabravo-oss/ab-provider-modules/pkg/resource"
)

type LiveOpts struct {
	ProjectIDs         []string
	ServiceAccountJSON []byte
	HTTPClient         *http.Client
	TokenSource        oauth2.TokenSource
	ComputeEndpoint    string
	StorageEndpoint    string
	SQLEndpoint        string
	ResourceEndpoint   string
}

func DiscoverLive(ctx context.Context, options LiveOpts) ([]resource.Resource, []string, error) {
	if len(options.ProjectIDs) == 0 {
		return nil, nil, fmt.Errorf("GCP projectIds required")
	}
	client := options.HTTPClient
	if options.TokenSource != nil {
		if client == nil {
			client = http.DefaultClient
		}
		authContext := context.WithValue(ctx, oauth2.HTTPClient, client)
		client = oauth2.NewClient(authContext, options.TokenSource)
	} else if client == nil {
		if len(options.ServiceAccountJSON) == 0 {
			return nil, nil, fmt.Errorf("GCP serviceAccountJson required")
		}
		credentials, err := google.CredentialsFromJSON(ctx, options.ServiceAccountJSON, "https://www.googleapis.com/auth/cloud-platform.read-only")
		if err != nil {
			return nil, nil, fmt.Errorf("GCP credentials: %w", err)
		}
		client = oauth2.NewClient(ctx, credentials.TokenSource)
	}
	computeBase := endpoint(options.ComputeEndpoint, "https://compute.googleapis.com/compute/v1")
	storageBase := endpoint(options.StorageEndpoint, "https://storage.googleapis.com/storage/v1")
	sqlBase := endpoint(options.SQLEndpoint, "https://sqladmin.googleapis.com/sql/v1beta4")
	resourceBase := endpoint(options.ResourceEndpoint, "https://cloudresourcemanager.googleapis.com/v1")
	var out []resource.Resource
	var serviceErrors []string
	for _, project := range options.ProjectIDs {
		project = strings.TrimSpace(project)
		if project == "" {
			continue
		}
		out = append(out, resource.Resource{Type: "cloud_account", UID: project, Account: project, Aliases: []resource.Alias{{Kind: "gcp_project_id", Value: project}}})
		instances, err := listGCPInstances(ctx, client, computeBase, project)
		if err != nil {
			serviceErrors = append(serviceErrors, "compute instances: "+err.Error())
		} else {
			out = append(out, instances...)
		}
		firewalls, err := listGCPFirewalls(ctx, client, computeBase, project)
		if err != nil {
			serviceErrors = append(serviceErrors, "compute firewalls: "+err.Error())
		} else {
			out = append(out, firewalls...)
		}
		buckets, err := listGCPBuckets(ctx, client, storageBase, project)
		if err != nil {
			serviceErrors = append(serviceErrors, "storage buckets: "+err.Error())
		} else {
			out = append(out, buckets...)
		}
		databases, err := listGCPSQL(ctx, client, sqlBase, project)
		if err != nil {
			serviceErrors = append(serviceErrors, "sql instances: "+err.Error())
		} else {
			out = append(out, databases...)
		}
		iam, err := getGCPIAM(ctx, client, resourceBase, project)
		if err != nil {
			serviceErrors = append(serviceErrors, "project IAM: "+err.Error())
		} else {
			out = append(out, iam)
		}
	}
	if len(out) == 0 {
		return nil, serviceErrors, fmt.Errorf("GCP discovery produced no project inventory")
	}
	serviceErrors = append(serviceErrors, "GCP preview covers Compute, firewall, Cloud Storage, Cloud SQL, and project IAM; remaining services are not authoritative")
	return out, serviceErrors, nil
}

type gcpInstance struct {
	ID                 string            `json:"id"`
	Name               string            `json:"name"`
	SelfLink           string            `json:"selfLink"`
	Zone               string            `json:"zone"`
	Status             string            `json:"status"`
	Labels             map[string]string `json:"labels"`
	DeletionProtection bool              `json:"deletionProtection"`
	ShieldedConfig     map[string]any    `json:"shieldedInstanceConfig"`
	NetworkInterfaces  []struct {
		AccessConfigs []struct {
			NatIP string `json:"natIP"`
		} `json:"accessConfigs"`
	} `json:"networkInterfaces"`
}

func listGCPInstances(ctx context.Context, client *http.Client, base, project string) ([]resource.Resource, error) {
	var out []resource.Resource
	token := ""
	for {
		var page struct {
			Items map[string]struct {
				Instances []gcpInstance `json:"instances"`
			} `json:"items"`
			Next string `json:"nextPageToken"`
		}
		requestURL := fmt.Sprintf("%s/projects/%s/aggregated/instances?maxResults=500", base, url.PathEscape(project))
		if token != "" {
			requestURL += "&pageToken=" + url.QueryEscape(token)
		}
		if err := getJSON(ctx, client, requestURL, &page); err != nil {
			return out, err
		}
		for scope, group := range page.Items {
			for _, instance := range group.Instances {
				uid := instance.SelfLink
				if uid == "" {
					uid = fmt.Sprintf("//compute.googleapis.com/projects/%s/%s/instances/%s", project, strings.TrimPrefix(scope, "zones/"), instance.Name)
				}
				public := []string{}
				for _, network := range instance.NetworkInterfaces {
					for _, access := range network.AccessConfigs {
						if access.NatIP != "" {
							public = append(public, access.NatIP)
						}
					}
				}
				out = append(out, resource.Resource{Type: "gcp.instance", UID: uid, Account: project, Region: lastPath(instance.Zone),
					Aliases: []resource.Alias{{Kind: "gcp_resource_name", Value: uid}},
					Attrs:   map[string]any{"name": instance.Name, "status": instance.Status, "labels": instance.Labels, "public_ipv4": public, "publicly_exposed": len(public) > 0, "deletion_protection": instance.DeletionProtection, "shielded_instance_config": instance.ShieldedConfig},
					Edges:   []resource.Edge{{From: project, To: uid, Kind: "contains"}},
				})
			}
		}
		if page.Next == "" {
			break
		}
		if page.Next == token {
			return out, fmt.Errorf("repeated instance page token %q", token)
		}
		token = page.Next
	}
	return out, nil
}

func listGCPFirewalls(ctx context.Context, client *http.Client, base, project string) ([]resource.Resource, error) {
	var out []resource.Resource
	token := ""
	for {
		var page struct {
			Items []struct {
				Name, SelfLink, Direction string
				Disabled                  bool
				SourceRanges              []string `json:"sourceRanges"`
				Allowed                   []struct {
					Protocol string   `json:"IPProtocol"`
					Ports    []string `json:"ports"`
				} `json:"allowed"`
			} `json:"items"`
			Next string `json:"nextPageToken"`
		}
		requestURL := fmt.Sprintf("%s/projects/%s/global/firewalls", base, url.PathEscape(project))
		if token != "" {
			requestURL += "?pageToken=" + url.QueryEscape(token)
		}
		if err := getJSON(ctx, client, requestURL, &page); err != nil {
			return out, err
		}
		for _, firewall := range page.Items {
			uid := nonEmpty(firewall.SelfLink, "gcp:firewall:"+project+":"+firewall.Name)
			out = append(out, resource.Resource{Type: "gcp.firewall", UID: uid, Account: project, Aliases: []resource.Alias{{Kind: "gcp_resource_name", Value: uid}}, Attrs: map[string]any{"name": firewall.Name, "direction": firewall.Direction, "disabled": firewall.Disabled, "source_ranges": firewall.SourceRanges, "allowed": firewall.Allowed}, Edges: []resource.Edge{{From: project, To: uid, Kind: "contains"}}})
		}
		if page.Next == "" {
			break
		}
		if page.Next == token {
			return out, fmt.Errorf("repeated firewall page token %q", token)
		}
		token = page.Next
	}
	return out, nil
}

func listGCPBuckets(ctx context.Context, client *http.Client, base, project string) ([]resource.Resource, error) {
	var out []resource.Resource
	token := ""
	for {
		var page struct {
			Items []struct {
				ID, Name, Location string
				IamConfiguration   struct {
					PublicAccessPrevention   string `json:"publicAccessPrevention"`
					UniformBucketLevelAccess struct {
						Enabled bool `json:"enabled"`
					} `json:"uniformBucketLevelAccess"`
				} `json:"iamConfiguration"`
				Encryption struct {
					DefaultKmsKeyName string `json:"defaultKmsKeyName"`
				} `json:"encryption"`
				RetentionPolicy map[string]any `json:"retentionPolicy"`
			} `json:"items"`
			Next string `json:"nextPageToken"`
		}
		requestURL := base + "/b?project=" + url.QueryEscape(project)
		if token != "" {
			requestURL += "&pageToken=" + url.QueryEscape(token)
		}
		if err := getJSON(ctx, client, requestURL, &page); err != nil {
			return out, err
		}
		for _, bucket := range page.Items {
			uid := "gs://" + bucket.Name
			out = append(out, resource.Resource{Type: "gcp.bucket", UID: uid, Account: project, Region: bucket.Location, Aliases: []resource.Alias{{Kind: "gcp_resource_name", Value: uid}}, Attrs: map[string]any{"public_access_prevention": bucket.IamConfiguration.PublicAccessPrevention, "uniform_bucket_level_access": bucket.IamConfiguration.UniformBucketLevelAccess.Enabled, "default_kms_key": bucket.Encryption.DefaultKmsKeyName, "retention_policy": bucket.RetentionPolicy}, Edges: []resource.Edge{{From: project, To: uid, Kind: "contains"}}})
		}
		if page.Next == "" {
			break
		}
		if page.Next == token {
			return out, fmt.Errorf("repeated bucket page token %q", token)
		}
		token = page.Next
	}
	return out, nil
}

func listGCPSQL(ctx context.Context, client *http.Client, base, project string) ([]resource.Resource, error) {
	var out []resource.Resource
	token := ""
	for {
		var page struct {
			Items []struct {
				Name, SelfLink, Region, DatabaseVersion, State string
				Settings                                       struct {
					BackupConfiguration map[string]any `json:"backupConfiguration"`
					IPConfiguration     struct {
						IPv4Enabled    bool   `json:"ipv4Enabled"`
						AuthorizedNets []any  `json:"authorizedNetworks"`
						RequireSSL     bool   `json:"requireSsl"`
						SSLMode        string `json:"sslMode"`
					} `json:"ipConfiguration"`
				} `json:"settings"`
			} `json:"items"`
			Next string `json:"nextPageToken"`
		}
		requestURL := fmt.Sprintf("%s/projects/%s/instances", base, url.PathEscape(project))
		if token != "" {
			requestURL += "?pageToken=" + url.QueryEscape(token)
		}
		if err := getJSON(ctx, client, requestURL, &page); err != nil {
			return out, err
		}
		for _, database := range page.Items {
			uid := nonEmpty(database.SelfLink, "gcp:sql:"+project+":"+database.Name)
			out = append(out, resource.Resource{Type: "gcp.sql", UID: uid, Account: project, Region: database.Region, Aliases: []resource.Alias{{Kind: "gcp_resource_name", Value: uid}}, Attrs: map[string]any{"name": database.Name, "database_version": database.DatabaseVersion, "state": database.State, "ipv4_enabled": database.Settings.IPConfiguration.IPv4Enabled, "authorized_networks": database.Settings.IPConfiguration.AuthorizedNets, "require_ssl": database.Settings.IPConfiguration.RequireSSL, "ssl_mode": database.Settings.IPConfiguration.SSLMode, "backup_configuration": database.Settings.BackupConfiguration}, Edges: []resource.Edge{{From: project, To: uid, Kind: "contains"}}})
		}
		if page.Next == "" {
			break
		}
		if page.Next == token {
			return out, fmt.Errorf("repeated SQL page token %q", token)
		}
		token = page.Next
	}
	return out, nil
}

func getGCPIAM(ctx context.Context, client *http.Client, base, project string) (resource.Resource, error) {
	var policy struct {
		Bindings []struct {
			Role    string   `json:"role"`
			Members []string `json:"members"`
		} `json:"bindings"`
		Version int `json:"version"`
	}
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/projects/%s:getIamPolicy", base, url.PathEscape(project)), bytes.NewReader([]byte(`{}`)))
	request.Header.Set("Content-Type", "application/json")
	if err := doJSON(client, request, &policy); err != nil {
		return resource.Resource{}, err
	}
	uid := "gcp:iam:" + project
	return resource.Resource{Type: "gcp.iam", UID: uid, Account: project, Attrs: map[string]any{"bindings": policy.Bindings, "version": policy.Version}, Edges: []resource.Edge{{From: project, To: uid, Kind: "contains"}}}, nil
}

func getJSON(ctx context.Context, client *http.Client, requestURL string, destination any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return err
	}
	return doJSON(client, request, destination)
}

func doJSON(client *http.Client, request *http.Request, destination any) error {
	const maxAttempts = 4
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		attemptRequest := request.Clone(request.Context())
		if request.GetBody != nil {
			body, err := request.GetBody()
			if err != nil {
				return err
			}
			attemptRequest.Body = body
		}
		response, err := client.Do(attemptRequest)
		if err != nil {
			return err
		}
		if response.StatusCode < 300 {
			defer response.Body.Close()
			return json.NewDecoder(io.LimitReader(response.Body, 64<<20)).Decode(destination)
		}
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		response.Body.Close()
		if attempt == maxAttempts || !retryableStatus(response.StatusCode) {
			return fmt.Errorf("HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
		}
		if err := waitRetry(request.Context(), response.Header.Get("Retry-After"), attempt); err != nil {
			return err
		}
	}
	return fmt.Errorf("HTTP retry attempts exhausted")
}

func retryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusInternalServerError || status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

func waitRetry(ctx context.Context, retryAfter string, attempt int) error {
	delay := time.Duration(attempt) * 100 * time.Millisecond
	if seconds, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && seconds >= 0 {
		delay = time.Duration(seconds) * time.Second
	}
	if delay > 2*time.Second {
		delay = 2 * time.Second
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func endpoint(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		value = fallback
	}
	return strings.TrimRight(value, "/")
}
func lastPath(value string) string {
	parts := strings.Split(strings.TrimRight(value, "/"), "/")
	return parts[len(parts)-1]
}
func nonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
