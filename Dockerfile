# syntax=docker/dockerfile:1

FROM golang:1.22-alpine AS builder

WORKDIR /src

RUN apk add --no-cache ca-certificates git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=
ARG TARGETOS=linux
ARG TARGETARCH

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
	go build -trimpath \
	-ldflags="-s -w -X github.com/sydneyowl/wsjtx-relay/internal/shared/buildinfo.Version=${VERSION} -X github.com/sydneyowl/wsjtx-relay/internal/shared/buildinfo.Commit=${COMMIT} -X github.com/sydneyowl/wsjtx-relay/internal/shared/buildinfo.Date=${BUILD_DATE}" \
	-o /out/wsjtx-relay-server ./cmd/wsjtx-relay-server

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
	go build -trimpath \
	-ldflags="-s -w -X github.com/sydneyowl/wsjtx-relay/internal/shared/buildinfo.Version=${VERSION} -X github.com/sydneyowl/wsjtx-relay/internal/shared/buildinfo.Commit=${COMMIT} -X github.com/sydneyowl/wsjtx-relay/internal/shared/buildinfo.Date=${BUILD_DATE}" \
	-o /out/wsjtx-relay-client ./cmd/wsjtx-relay-client

FROM alpine:3.21 AS runtime-base

RUN apk add --no-cache ca-certificates
WORKDIR /app

FROM runtime-base AS server

COPY --from=builder /out/wsjtx-relay-server /usr/local/bin/wsjtx-relay-server
RUN mkdir -p /data && chown -R nobody:nobody /data

USER nobody:nobody
EXPOSE 8443
VOLUME ["/data"]
ENTRYPOINT ["/usr/local/bin/wsjtx-relay-server"]

FROM runtime-base AS client

COPY --from=builder /out/wsjtx-relay-client /usr/local/bin/wsjtx-relay-client
RUN mkdir -p /data && chown -R nobody:nobody /data

USER nobody:nobody
EXPOSE 2237/udp
VOLUME ["/data"]
ENTRYPOINT ["/usr/local/bin/wsjtx-relay-client"]
