# AB provider modules

Shared Go cloud discovery for AlphaBravo products. Provider packages collect cloud data; consuming products own authorization, persistence, policy, operations and UI.

Derived from `AlphaBravoCompany/sierra-scan-provider-modules` commit `38a085abaacdab74c20257db1962eac0ecb8bd69`. This is a clean source export; the original private repository and Sierra consumer are unchanged.

## Included providers

AWS, Azure, GCP, OCI, Linode, DigitalOcean and Hetzner collectors are included. Coverage differs by provider; this is not a claim of complete cloud API support or live verification. Hetzner's normalized discovery currently covers projects and servers.

Import the Go packages directly inside a product's worker, or build the `cmd/ab-provider-*` commands. Standalone commands accept one bounded JSON request over stdin/stdout using `ab.provider/v1` (`discover` or `health`). The protocol and command names have no Sierra compatibility aliases.

`providers/hetzner.ListServers` supplies complete SDK server records with an explicit resource bound. It discards results on any page failure or invalid pagination. Providah uses this function with its own credentials, HTTP transport, region filtering and resource mapping.

Normalized `DiscoverLive` results may contain partial service errors. Callers must preserve these errors and must not interpret incomplete discovery as resource deletion or a clean security scan. The generic resource format includes security-oriented attributes inherited from the original collectors; consumers need not adopt it as their database model.

## Build and check

Go version is pinned by `go.mod`.

```sh
go test ./...
go vet ./...
go build ./cmd/...
```

Checks use recorded/local HTTP responses, not live cloud mutations. No hosted provider service or package installation framework is required. Consumers pin an exact module version and compile only the packages they use into their own workers.

Use read-only provider credentials. The AWS permission starting point is in `permissions/aws-provider-read.json`; verify permissions against the specific collection scope. Product-controlled endpoints, transport and credential policies remain the consumer's responsibility.

## SDK updates and releases

Upstream cloud SDKs stay ordinary Go module dependencies; this repository adds collection, pagination and normalization rather than vendoring or forking SDKs. Some collectors also use provider HTTP APIs where appropriate. Public functions may expose upstream SDK types, so a major SDK change requires consumer review.

Dependabot opens weekly Go dependency and GitHub Actions update PRs. AWS, Azure and Google module families are grouped to reduce mismatched SDK updates. CI verifies modules, runs recorded adapter checks and Go vet, and checks known vulnerabilities. Review and merge passing updates, then publish a new immutable semantic version tag. No automatic merge or floating dependency in consuming releases.

For a targeted manual SDK update:

```sh
go get github.com/hetznercloud/hcloud-go/v2@latest
go mod tidy
go test ./...
go vet ./...
```

After a tested module release, consumers update explicitly:

```sh
go get github.com/alphabravo-oss/ab-provider-modules@v0.1.0
go mod tidy
```

Run the consumer's affected worker checks too: Go may select newer SDK versions already required by that product. Existing releases remain pinned until updated. Go toolchain updates are a separate `go.mod`/build-image change; dependency PRs are not a promise to update the compiler automatically.

Licensed under Apache-2.0; see [LICENSE](LICENSE).
