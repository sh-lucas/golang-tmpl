# Stage 1: build Go binary
FROM golang:1.27-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG CGO_ENABLED=0
RUN test "$CGO_ENABLED" = "0" && CGO_ENABLED="$CGO_ENABLED" GOOS=linux go build -buildvcs=false -trimpath -ldflags="-s -w" -o /backend ./cmd/backend

# Stage 2: minimal Go backend
FROM scratch

COPY --from=builder /backend /backend

VOLUME ["/data"]

ENV DB_PATH=/data/app.db
ENV ADDR=:3000

EXPOSE 3000

ENTRYPOINT ["/backend"]
