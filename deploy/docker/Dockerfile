FROM golang:1.26-alpine AS base
WORKDIR /app
COPY . .
RUN go build -o /usr/local/bin/aegis-server ./cmd/server

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=base /usr/local/bin/aegis-server /usr/local/bin/aegis-server
COPY migrations ./migrations
COPY .env.example ./.env.example
CMD ["/usr/local/bin/aegis-server"]
