# Dev-compose unbound-stats scraper sidecar (context: repo root). The
# distroless backend image can't host this: the scraper shells out to
# unbound-control and needs a loop shell.
FROM golang:alpine AS build
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /v6ctl ./cmd/v6ctl

FROM alpine
RUN apk add --no-cache unbound
COPY --from=build /v6ctl /usr/local/bin/v6ctl
COPY deploy/unbound/dev/unbound-control.sh /usr/local/bin/unbound-control-compose
COPY deploy/unbound/dev/control-client.conf /etc/unbound/control-client.conf
ENTRYPOINT ["/bin/sh", "-c", "while :; do v6ctl ops unbound-stats; sleep 60; done"]
