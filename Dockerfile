# syntax=docker/dockerfile:1
# Base images are pinned by digest; Dependabot keeps the digests current.

FROM node:22-alpine@sha256:fe4f6d74e20370acb04ad889077d14d0f64a9c8ea6a42bc60cddf1e844699914 AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.27-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X github.com/floriandejonckheere/dropcatch/internal/api.Version=${VERSION}" \
      -o /out/dropcatch ./cmd/dropcatch

# Python for the Apprise CLI, which sends the notifications.
FROM python:3.13-alpine@sha256:bf12ecc0bea3f171d55145dd02c78b5eeb09f30ee2ac9cc2b4c9e4e8f5e30741
COPY docker/requirements.txt /tmp/requirements.txt
RUN apk add --no-cache su-exec tzdata \
 && pip install --no-cache-dir -r /tmp/requirements.txt \
 && rm /tmp/requirements.txt \
 && mkdir -p /data
COPY --from=build /out/dropcatch /usr/local/bin/dropcatch
COPY docker/entrypoint.sh docker/healthcheck.sh /usr/local/bin/
# The entrypoint drops privileges to PUID:PGID (default 1000:1000).
ENV PUID=1000 \
    PGID=1000 \
    HOME=/tmp \
    DROPCATCH_DATA_DIR=/data \
    DROPCATCH_LISTEN=:8080
EXPOSE 8080
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=3s CMD ["healthcheck.sh"]
ENTRYPOINT ["entrypoint.sh"]
