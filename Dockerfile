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

VOLUME ["/data"]

ENV DATABASE_ROOT=/data
ENV SERVER_PORT=3000
ENV JWT_SECRET=""
ENV DATABASE_ACCESS_KEY=""
ENV BOOK_ROOT=/book

EXPOSE 3000

ENTRYPOINT ["/backend"]
