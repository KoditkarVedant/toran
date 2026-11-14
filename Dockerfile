# syntax=docker/dockerfile:1
# Multi-arch image: docker buildx build --platform linux/amd64,linux/arm64,linux/arm/v7 ...
FROM --platform=$BUILDPLATFORM golang:1.22-alpine AS build
ARG TARGETOS TARGETARCH TARGETVARIANT
WORKDIR /src
COPY server/go.mod ./
COPY server/*.go ./
RUN GOARM="${TARGETVARIANT#v}" CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags '-s -w' -o /toran .

FROM alpine:3
ARG VERSION=dev
LABEL org.opencontainers.image.title="toran" \
      org.opencontainers.image.description="A fast start page for your home server" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="$VERSION"
COPY --from=build /toran /usr/local/bin/toran
# the page ships in /opt/toran; the entrypoint copies it into the /data volume,
# which is also where services.json, backups and uploads live
COPY index.html custom.css services.sample.json /opt/toran/
COPY wallpapers /opt/toran/wallpapers
COPY docker/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh
ENV ROOT=/data ADDR=:8080 GOMEMLIMIT=24MiB
VOLUME /data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s CMD wget -qO- "http://127.0.0.1:${ADDR#:}/api/health" >/dev/null || exit 1
ENTRYPOINT ["entrypoint.sh"]
