# syntax=docker/dockerfile:1

FROM golang:1.27-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -buildvcs=false -o /my-usage ./cmd/server

FROM scratch
# The providers call HTTPS endpoints and scratch ships no trust store.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /my-usage /my-usage
EXPOSE 9090
ENV MY_USAGE_ACCOUNTS_DIR=/accounts MY_USAGE_LISTEN=:9090
# Read-write: the tool rewrites an account file when it refreshes its token.
VOLUME /accounts
ENTRYPOINT ["/my-usage"]
