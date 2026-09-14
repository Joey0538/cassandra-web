# Builds from the build context rather than cloning from GitHub, so a local
# change is actually in the image you just built.
#
# The upstream Dockerfile can no longer build at all: its final stage pulled
# cqlsh from downloads.datastax.com/enterprise/cqlsh-astra.tar.gz, which now
# 404s, and installed python2, which Alpine dropped after 3.16. Describe is
# served over native CQL instead (Cassandra 4.0+), so neither is needed.

# build client stage
#
# Pinned to Node 16: the client is Vue 2 on vue-cli 3 / webpack 4 with
# node-sass, which does not build on Node 20+. alpine3.18 is the newest Alpine
# Node 16 was ever published on, so this stage cannot track Alpine the way the
# other two do. Modernising the frontend is a separate job from this one.
FROM node:16.20.2-alpine3.18 AS build-client-env

# node-sass compiles a native addon.
RUN apk add --no-cache python3 make g++ git

WORKDIR /src/client
COPY client/package.json client/package-lock.json* ./
RUN npm install --force

COPY client/ ./
RUN npm run build

# build server stage
FROM golang:1.26.8-alpine3.24 AS build-server-env

WORKDIR /src
COPY go.mod go.sum ./
COPY vendor/ ./vendor/
COPY service/ ./service/

# Static binary, so the runtime stage needs no libc shim for it.
RUN CGO_ENABLED=0 GOOS=linux go build -mod vendor -trimpath \
    -ldflags "-s -w" -o /out/service ./service

# final stage
FROM alpine:3.24.1

# Backs the UI's export/import buttons. gcompat is for these two glibc-linked
# binaries, not for the Go service.
ADD https://github.com/masumsoft/cassandra-exporter/releases/download/v1.0.4/cassandra-exporter-linux.zip /tmp/cassandra-exporter.zip
RUN apk add --no-cache gcompat unzip \
    && unzip /tmp/cassandra-exporter.zip -d /tmp \
    && mv /tmp/cassandra-exporter-linux/export-linux /sbin/cexport \
    && mv /tmp/cassandra-exporter-linux/import-linux /sbin/cimport \
    && chmod +x /sbin/cexport /sbin/cimport \
    && rm -rf /tmp/cassandra-exporter.zip /tmp/cassandra-exporter-linux \
    && apk del unzip

COPY --from=build-server-env /out/service /service
COPY service/config.yaml /config.yaml
COPY --from=build-client-env /src/client/dist /client/dist

RUN adduser -D nonroot
ENV HOME=/home/nonroot
USER nonroot

WORKDIR /

EXPOSE 8083

CMD ["/service"]
