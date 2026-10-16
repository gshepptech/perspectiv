# syntax=docker/dockerfile:1.28
#
# One image for both Deployments: /perspectiv serves per ROLE (api, worker,
# all) and runs `perspectiv migrate`; /placectl imports place data.
#
#   docker build \
#     --build-arg GO_VERSION="$(sed -n 's/^toolchain go//p' go.mod)" \
#     --build-arg VERSION="$(git describe --tags --always --dirty)" \
#     --build-arg COMMIT="$(git rev-parse --short HEAD)" \
#     --build-arg DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
#     --build-arg SOURCE_URL="$(git remote get-url origin)" \
#     -t perspectiv:dev .

ARG GO_VERSION=1.27.2
ARG NODE_VERSION=24.22.0

# --- web: the SPA bundle the server embeds -----------------------------------
FROM node:${NODE_VERSION}-alpine AS web
WORKDIR /src/web
RUN corepack enable pnpm
COPY web/package.json web/pnpm-lock.yaml* ./
RUN if [ -f pnpm-lock.yaml ]; then pnpm install --frozen-lockfile; else pnpm install; fi
# Host build output and dependencies never enter the image build.
COPY --exclude=node_modules --exclude=dist web/ ./
RUN pnpm run build

# --- go-deps: toolchain check and module download -----------------------------
FROM golang:${GO_VERSION}-alpine AS go-deps
ARG GO_VERSION
# Never let the go command fetch a different toolchain than this image's.
ENV GOTOOLCHAIN=local CGO_ENABLED=0
WORKDIR /src
COPY go.mod go.sum ./
# The image's Go must be the toolchain go.mod pins; a mismatch fails the build.
RUN want="$(sed -n 's/^toolchain go//p' go.mod)"; \
    have="$(go env GOVERSION)"; \
    if [ "go${want}" != "${have}" ]; then \
      echo "Dockerfile GO_VERSION ${GO_VERSION} gives ${have}, but go.mod pins toolchain go${want}" >&2; \
      exit 1; \
    fi
RUN --mount=type=cache,target=/go/pkg/mod go mod download

# --- build: both binaries, static, version-stamped ----------------------------
FROM go-deps AS build
ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY pkg/ ./pkg/
COPY gen/ ./gen/
COPY web/*.go ./web/
COPY --from=web /src/web/dist ./web/dist
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    BI=perspectiv/internal/api/buildinfo; \
    LDFLAGS="-s -w -X ${BI}.Version=${VERSION} -X ${BI}.Commit=${COMMIT} -X ${BI}.Date=${DATE}"; \
    go build -trimpath -ldflags "${LDFLAGS}" -o /out/perspectiv ./cmd/perspectiv && \
    go build -trimpath -ldflags "${LDFLAGS}" -o /out/placectl ./cmd/placectl

# --- runtime: distroless, non-root --------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot AS runtime
ARG VERSION=dev
ARG SOURCE_URL=""
LABEL org.opencontainers.image.source="${SOURCE_URL}" \
      org.opencontainers.image.title="perspectiv" \
      org.opencontainers.image.version="${VERSION}"
COPY --from=build /out/perspectiv /perspectiv
COPY --from=build /out/placectl /placectl
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/perspectiv"]
