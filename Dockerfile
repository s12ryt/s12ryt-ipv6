FROM node:24-bookworm-slim AS web-build

WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.25-bookworm AS go-build

ARG TARGETARCH
WORKDIR /src
COPY . ./
COPY --from=web-build /src/web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/s12ryt-ipv6 ./cmd/s12ryt-ipv6

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /etc/s12ryt-ipv6 \
    && chmod 0700 /etc/s12ryt-ipv6
COPY --from=go-build /out/s12ryt-ipv6 /usr/local/bin/s12ryt-ipv6

VOLUME ["/etc/s12ryt-ipv6"]
ENTRYPOINT ["/usr/local/bin/s12ryt-ipv6"]
CMD ["serve", "--data-dir", "/etc/s12ryt-ipv6"]
