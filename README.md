<h1 align="center">S3 Panel</h1>

<p align="center">
<img alt="GitHub Actions Workflow Status" src="https://img.shields.io/github/actions/workflow/status/snapp-incubator/s3-panel/ci.yml?style=for-the-badge&logo=github">
<img alt="GitHub Release" src="https://img.shields.io/github/v/release/snapp-incubator/s3-panel?style=for-the-badge&logo=rocket">
<img alt="GitHub License" src="https://img.shields.io/github/license/snapp-incubator/s3-panel?style=for-the-badge&logo=gnu">
</p>

S3 Panel is a self-service web panel for S3-compatible object storage (Ceph RGW):
authenticate and manage buckets and objects — list, upload, download, delete and
share — and view quotas.

## Authentication modes

`server.auth_mode` selects how a caller is identified.

**`s3` (default)** — the user supplies their own S3 access key and secret key.
The gateway is the only authorization layer, so a user sees exactly the buckets
their own account owns. This is the historical behaviour and is unchanged.

**`iam`** — the user signs in with OIDC, and an S3-compatible *control endpoint*
answers which buckets they may use and with what permission. The panel speaks
only standard protocols — OIDC, S3 and STS — so any service implementing them can
back it.

```
browser ──OIDC──▶ panel ──S3 (ListBuckets, ?policy, ?stats)──▶ control endpoint
                    │  ──STS GetSessionToken─────────────────▶
                    ▼
              short-lived credential
                    │
browser/panel ──────┴──S3 objects──▶ gateway   (the control endpoint is not in this path)
```

What this adds over `s3` mode:

- **Buckets from every region in one list**, including buckets the user does not
  own but has been granted access to.
- **Per-bucket permissions** (`read` / `write` / `owner`), shown in the UI and
  enforced server-side on every data-plane route.
- **A bucket detail page** with object count, size, quota and the bucket policy.
- **An admin view** for members of a configured group.

Object bytes never pass through the control endpoint: it authorizes and vends a
short-lived credential, and the client signs its object calls straight to the
gateway.

Bucket creation and per-user quota are unavailable in `iam` mode — a minted
credential belongs to no tenant, and a user has no single gateway account to
carry a quota. See `configs/sample-config.toml` for the full configuration.

This is a monorepo:

- **Backend** (`cmd/`, `internal/`) — a Go HTTP API in front of the Ceph RADOS Gateway.
- **Frontend** (`frontend/`) — a Vite + React + TypeScript UI, embedded into and
  served by the backend binary (`internal/web`), so there is a single image.
- **Deploy** (`deploy/helm/`) — the `s3-panel` Helm chart.

## Getting started

Run the backend with a config file:

```sh
go run ./cmd/s3-panel s3-panel --configPath=./configs/sample-config.toml
```

Configuration is TOML, loaded with [koanf](https://github.com/knadh/koanf) (defaults
< config file < `s3panel_`-prefixed environment variables). See
`configs/sample-config.toml` for the format and `frontend/README.md` for the UI.

## Deployment

A Helm chart lives in [`deploy/helm`](deploy/helm) (chart name `s3-panel`). On each
semver tag the release workflow publishes it as an OCI artifact alongside the
single image (which serves both the API and the embedded frontend):

| Artifact | Reference |
| --- | --- |
| Image (API + UI) | `ghcr.io/snapp-incubator/s3-panel:<version>` |
| Helm chart (OCI) | `oci://ghcr.io/snapp-incubator/charts/s3-panel` |

Install from the OCI registry:

```sh
helm install s3-panel oci://ghcr.io/snapp-incubator/charts/s3-panel \
  --version <version> -n snappcloud-unified-panel -f my-values.yaml
```

## CI

GitHub Actions (`.github/workflows`):

- **CI** (`ci.yml`) — **lint** (golangci-lint, Biome, `helm lint --strict`) and **test**
  (`go test`, plus chart manifest validation with kubeconform), then **build** the single
  image. The build job runs only after lint and test pass.
- **Release** (`release.yml`) — on a semver tag, builds and pushes the image and the Helm
  chart (OCI) to GHCR.

## API

The HTTP API is documented with Swagger/OpenAPI. With the server running, browse the
interactive docs at `/docs/` (e.g. <http://127.0.0.1:8080/docs/>); the generated spec lives
in [`docs/swagger.yaml`](docs/swagger.yaml).
