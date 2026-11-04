# Where base images, npm packages and Go modules come from. The defaults are
# the public ones; a build behind a mirror (e.g. the fork's GitLab CI) overrides
# them with --build-arg and gets the same image.
ARG NODE_IMAGE=node:22-alpine
ARG GO_IMAGE=golang:1.27-alpine
ARG RUNTIME_IMAGE=alpine:3.24
ARG NPM_REGISTRY=https://registry.npmjs.org/
ARG GOPROXY=https://proxy.golang.org,direct

# 1) Build the frontend (Vite) into /fe/dist.
FROM ${NODE_IMAGE} AS frontend
ARG NPM_REGISTRY
# npm (installing corepack), corepack (fetching pnpm) and pnpm all read these.
ENV npm_config_registry=${NPM_REGISTRY} \
    COREPACK_NPM_REGISTRY=${NPM_REGISTRY}

# Build-time configuration, exposed to Vite as environment variables.
ARG VITE_API_LANGUAGE=fa
ARG VITE_APP_ENVIRONMENT
ARG VITE_AUTH_TOKEN
ARG VITE_VARIANT=cab
ARG VITE_CENTRAL_BACKEND_API
ARG VITE_ENV
# Path prefix the panel is served under, e.g. /s3-panel. Empty = the host root.
ARG VITE_BASE_PATH
# Release version (the git tag), shown in the UI. Falls back to package.json.
ARG APP_VERSION
ENV VITE_API_LANGUAGE=$VITE_API_LANGUAGE \
    VITE_APP_ENVIRONMENT=$VITE_APP_ENVIRONMENT \
    VITE_AUTH_TOKEN=$VITE_AUTH_TOKEN \
    VITE_VARIANT=$VITE_VARIANT \
    VITE_CENTRAL_BACKEND_API=$VITE_CENTRAL_BACKEND_API \
    VITE_ENV=$VITE_ENV \
    VITE_BASE_PATH=$VITE_BASE_PATH

# Corepack is unbundled from Node 25+, so install it explicitly (works on any
# Node major); it activates the pnpm version pinned in package.json.
RUN apk add --no-cache git && npm install -g corepack@latest && corepack enable
WORKDIR /fe
COPY frontend/package.json frontend/pnpm-lock.yaml frontend/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY frontend/ ./
# Stamp the release version into package.json so the UI (vite-plugin-package-version)
# shows the tag instead of the committed placeholder.
RUN if [ -n "$APP_VERSION" ]; then npm pkg set version="$APP_VERSION"; fi
RUN pnpm run build

# 2) Build the Go binary with the frontend embedded (internal/web/dist).
FROM ${GO_IMAGE} AS builder
ARG GOPROXY
ENV GOPROXY=${GOPROXY}
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /fe/dist ./internal/web/dist
# The release version again, for the backend (it reports it when it registers
# with a console). Empty falls back to the module version, then "dev".
ARG APP_VERSION
RUN go build -v -ldflags "-X github.com/snapp-incubator/S3-Panel/internal/version.Version=${APP_VERSION}" -o s3-panel ./cmd/s3-panel

# 3) Minimal runtime image — a single binary that serves the API and the SPA.
FROM ${RUNTIME_IMAGE}
WORKDIR /app/
COPY --from=builder /app/s3-panel .
ENTRYPOINT ["./s3-panel"]
CMD ["s3-panel", "--configPath=./config.toml"]
