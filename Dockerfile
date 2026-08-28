# Stage 1: build Go binary
FROM golang:1.27-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -trimpath -ldflags="-s -w" -o /backend ./cmd/backend

# Stage 2: Go backend
FROM alpine:3.23

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /backend /backend

VOLUME ["/data"]

ENV DB_PATH=/data/app.db
ENV ADDR=:3000

EXPOSE 3000

WORKDIR /

ENTRYPOINT ["/backend"]
