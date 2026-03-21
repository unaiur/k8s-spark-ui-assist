# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN gofmt -l . | grep . && exit 1 || true
RUN go vet ./...
RUN go build -trimpath -ldflags="-s -w" -o /spark-ui-assist ./cmd/spark-ui-assist

# Runtime stage — distroless/static for minimal footprint
FROM gcr.io/distroless/static:nonroot

COPY --from=builder /spark-ui-assist /spark-ui-assist

USER nonroot:nonroot
ENTRYPOINT ["/spark-ui-assist"]
