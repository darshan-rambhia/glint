FROM golang:1.26-alpine AS builder
RUN apk add --no-cache git gcc musl-dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go install github.com/a-h/templ/cmd/templ@latest && templ generate
ARG VERSION=dev
RUN CGO_ENABLED=1 go build \
    -ldflags="-s -w -linkmode external -extldflags '-static' \
      -X main.version=${VERSION} \
      -X main.commit=$(git rev-parse HEAD 2>/dev/null || echo unknown) \
      -X main.buildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o /glint ./cmd/glint

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /glint /usr/local/bin/glint
COPY --from=builder /src/static /static
USER 65534:65534
EXPOSE 3800
VOLUME ["/data"]
ENTRYPOINT ["glint"]
