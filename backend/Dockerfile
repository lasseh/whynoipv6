FROM golang:alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /api     ./cmd/api && \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o /crawler ./cmd/crawler && \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o /v6ctl   ./cmd/v6ctl

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /api /crawler /v6ctl /
ENTRYPOINT ["/api"]
