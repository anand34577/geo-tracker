# Multi-stage, multi-arch build: node builds the UI, Go cross-compiles one static
# binary with the UI embedded, and the final image is distroless (no shell, non-root).
# docker buildx build --platform linux/amd64,linux/arm64,linux/arm/v7 -t geotracker .

FROM --platform=$BUILDPLATFORM node:lts-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY web/embed.go ./web/
COPY --from=web /src/web/dist ./web/dist
ARG TARGETOS TARGETARCH TARGETVARIANT VERSION=dev
RUN GOARM=${TARGETVARIANT#v} CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/geotracker ./cmd/geotracker \
 && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/geotracker /geotracker
COPY --from=build --chown=nonroot:nonroot /out/data /data
ENV GT_DATA_DIR=/data GT_LISTEN=:8080
VOLUME /data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s CMD ["/geotracker", "healthcheck"]
USER nonroot
ENTRYPOINT ["/geotracker"]
