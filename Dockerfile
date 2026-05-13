FROM golang:1.23-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags="-s -w" \
    -o wsjtx-relay-server \
    ./cmd/wsjtx-relay-server

FROM alpine:latest

COPY --from=builder /build/wsjtx-relay-server /usr/local/bin/wsjtx-relay-server

EXPOSE 8443

ENTRYPOINT ["/usr/local/bin/wsjtx-relay-server"]
