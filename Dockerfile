# Where base images, Alpine packages, npm packages and Go modules come from.
# Unset, everything comes from the public defaults exactly as before; a build
# behind mirrors sets them with --build-arg.
ARG NODE_IMAGE=node:22-alpine
ARG GO_IMAGE=golang:1.27-alpine
ARG RUNTIME_IMAGE=alpine:3.24
# e.g. https://mirror.example/repository/alpine, standing in for
# https://dl-cdn.alpinelinux.org/alpine.
ARG ALPINE_MIRROR=
# An npm registry for npm, corepack (fetching pnpm) and pnpm.
ARG NPM_REGISTRY=
ARG GOPROXY=https://proxy.golang.org,direct

# 1) Build the frontend (Vite) into /fe/dist.
FROM ${NODE_IMAGE} AS frontend
ARG ALPINE_MIRROR
ARG NPM_REGISTRY

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
#
# The registry goes on the command line, not in npm_config_registry: pnpm 11
# ignores npm's variables. corepack replaces the "https://registry.npmjs.org"
# prefix of pnpm's tarball URL with its registry, hence no trailing slash.
RUN if [ -n "$ALPINE_MIRROR" ]; then \
      sed -i -E "s#https?://dl-cdn.alpinelinux.org/alpine#${ALPINE_MIRROR%/}#" /etc/apk/repositories; \
    fi && \
    apk add --no-cache git && \
    npm install -g ${NPM_REGISTRY:+--registry=$NPM_REGISTRY} corepack@latest && \
    corepack enable
WORKDIR /fe
COPY frontend/package.json frontend/pnpm-lock.yaml frontend/pnpm-workspace.yaml ./
RUN if [ -n "$NPM_REGISTRY" ]; then export COREPACK_NPM_REGISTRY="${NPM_REGISTRY%/}"; fi && \
    pnpm install --frozen-lockfile ${NPM_REGISTRY:+--registry=$NPM_REGISTRY}
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
