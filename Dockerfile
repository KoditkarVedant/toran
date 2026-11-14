# syntax=docker/dockerfile:1
# Multi-arch image: docker buildx build --platform linux/amd64,linux/arm64,linux/arm/v7 ...
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build
ARG TARGETOS TARGETARCH TARGETVARIANT VERSION=0.1.0
WORKDIR /src
COPY server/go.mod ./
COPY server/*.go ./
RUN GOARM="${TARGETVARIANT#v}" CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /toran .
# an empty folder to become /data in the final image (it cannot be made there: that stage has no RUN)
RUN mkdir /data-template

FROM alpine:3
ARG VERSION=0.1.0
LABEL org.opencontainers.image.title="toran" \
      org.opencontainers.image.description="A fast start page for your home server" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="$VERSION"
COPY --from=build /toran /usr/local/bin/toran
# the page ships in /opt/toran; the entrypoint copies it into the /data volume,
# which is also where services.json, backups and uploads live
COPY index.html custom.css services.sample.json /opt/toran/
COPY wallpapers /opt/toran/wallpapers
# --chmod instead of RUN keeps this stage free of RUN steps, so arm images build without emulation
COPY --chmod=755 docker/entrypoint.sh /usr/local/bin/entrypoint.sh
# Run as an ordinary user, not root. 65532 is the usual "nobody-like" id for such images; it needs
# no entry in /etc/passwd. /data is created owned by it, so a new volume mounted there is writable.
# (A volume that already exists and belongs to root must be handed over once: see docs/deploy.md.)
COPY --from=build --chown=65532:65532 /data-template /data
USER 65532:65532
ENV ROOT=/data ADDR=:8080 GOMEMLIMIT=24MiB
VOLUME /data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s CMD wget -qO- "http://127.0.0.1:${ADDR#:}/api/health" >/dev/null || exit 1
ENTRYPOINT ["entrypoint.sh"]
