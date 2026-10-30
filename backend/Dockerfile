# --platform=$BUILDPLATFORM pins the build stage to the runner's own
# architecture and lets Go cross-compile to $TARGETARCH. Without it buildx
# emulates the whole stage under QEMU for a multi-arch build — the Go
# compiler included — which is minutes per architecture, and needs a
# setup-qemu-action the release workflow does not configure.
FROM --platform=$BUILDPLATFORM golang:alpine AS build
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# The context has no .git, so the toolchain cannot stamp the revision. Below the
# module download so a new commit keeps that layer cached.
ARG REVISION
ENV LDFLAGS="-s -w -X github.com/lasseh/whynoipv6/internal/buildinfo.stamped=$REVISION"
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -ldflags="$LDFLAGS" -o /api     ./cmd/api && \
    CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -ldflags="$LDFLAGS" -o /crawler ./cmd/crawler && \
    CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -ldflags="$LDFLAGS" -o /v6ctl   ./cmd/v6ctl

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /api /crawler /v6ctl /
ENTRYPOINT ["/api"]
