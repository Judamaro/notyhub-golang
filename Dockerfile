# syntax=docker/dockerfile:1

FROM docker.io/library/golang:1.26-alpine AS build
WORKDIR /src

# Download modules first so the dependency layer is cached between code changes.
COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ cmd/
COPY internal/ internal/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/notyhub ./cmd/notyhub

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
# 8080 serves the API, the hub and /health; 9464 serves only the Prometheus metrics and must not be published.
ENV HTTP_ADDR=":8080" \
    METRICS_ADDR=":9464"
EXPOSE 8080 9464
COPY --from=build /out/notyhub /notyhub
USER nonroot:nonroot

# The image has no shell or curl: the binary checks GET /health itself.
HEALTHCHECK --interval=30s --timeout=5s --retries=3 CMD ["/notyhub", "healthcheck"]
ENTRYPOINT ["/notyhub"]
