# Stage 1: build Go binary
FROM golang:1.27-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN mkdir -p /src/book
RUN go test ./...

ARG CGO_ENABLED=0
RUN test "$CGO_ENABLED" = "0" && CGO_ENABLED="$CGO_ENABLED" GOOS=linux go build -buildvcs=false -trimpath -ldflags="-s -w" -o /backend ./cmd/backend

# Stage 2: minimal Go backend
FROM scratch

COPY --from=builder /backend /backend
COPY --from=builder /src/book /book
# TLS roots for outbound HTTPS (health probe, Litestream S3).
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

VOLUME ["/data"]

ENV DATABASE_ROOT=/data
ENV SERVER_PORT=3000
ENV JWT_SECRET=""
ENV DATABASE_ACCESS_KEY=""
ENV BOOK_ROOT=/book

EXPOSE 3000

ENTRYPOINT ["/backend"]

# Litestream (optional, disabled): continuous backup of /data/sqlite.db.
# To enable, uncomment the three lines below and set at runtime (never in the image):
#   LITESTREAM_REPLICA_URL        e.g. s3://my-bucket/golang-tmpl (the scheme is required)
#   LITESTREAM_ACCESS_KEY_ID      S3 credentials
#   LITESTREAM_SECRET_ACCESS_KEY
# On start with an empty /data it restores the latest backup before the app runs;
# an existing database is never overwritten, and replica errors stop the start.
# One replica URL per deployment: never point two running instances at the same one.
# large_blobs/ is not replicated. See specs/agents/operations.md.
# COPY --from=litestream/litestream:0.5.11 /usr/local/bin/litestream /litestream
# COPY litestream.yml /etc/litestream.yml
# ENTRYPOINT ["/litestream", "replicate", "-exec", "/backend"]
