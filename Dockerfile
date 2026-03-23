# Build stage
ARG GO_VERSION=1.26.1
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN gofmt -l . | grep . && exit 1 || true
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /spark-ui-assist ./cmd/spark-ui-assist

# Runtime stage — distroless/static for minimal footprint
FROM gcr.io/distroless/static:nonroot

COPY --from=builder /spark-ui-assist /spark-ui-assist

USER nonroot:nonroot
ENTRYPOINT ["/spark-ui-assist"]
CMD ["--help"]
