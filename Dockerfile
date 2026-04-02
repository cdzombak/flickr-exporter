ARG BIN_NAME=flickr-exporter
ARG BIN_VERSION=<unknown>

FROM golang:1-alpine AS builder
ARG BIN_NAME
ARG BIN_VERSION
WORKDIR /src/flickr-exporter
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-X main.version=${BIN_VERSION}" -o ./out/${BIN_NAME} .

FROM alpine:latest
ARG BIN_NAME
ARG BIN_VERSION
RUN apk add --no-cache ca-certificates perl exiftool
COPY --from=builder /src/flickr-exporter/out/${BIN_NAME} /usr/bin/flickr-exporter
ENTRYPOINT ["/usr/bin/flickr-exporter"]

LABEL license="GPL-3.0"
LABEL maintainer="Chris Dzombak <https://www.dzombak.com>"
LABEL org.opencontainers.image.authors="Chris Dzombak <https://www.dzombak.com>"
LABEL org.opencontainers.image.url="https://github.com/cdzombak/flickr-exporter"
LABEL org.opencontainers.image.documentation="https://github.com/cdzombak/flickr-exporter/blob/main/README.md"
LABEL org.opencontainers.image.source="https://github.com/cdzombak/flickr-exporter.git"
LABEL org.opencontainers.image.version="${BIN_VERSION}"
LABEL org.opencontainers.image.licenses="GPL-3.0"
LABEL org.opencontainers.image.title="${BIN_NAME}"
LABEL org.opencontainers.image.description="A command-line tool to download and archive your Flickr photos with metadata preservation"
