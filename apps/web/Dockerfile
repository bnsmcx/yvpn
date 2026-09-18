# Builds the whole web app: the Go server that serves index.html and relays the
# Tailscale API from the same origin. Build context is the repo root:
#   docker build -f apps/web/Dockerfile .
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY apps/web/proxy/ ./
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/yvpn-web .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 yvpn
COPY --from=build /out/yvpn-web /usr/local/bin/yvpn-web
COPY apps/web/index.html /app/index.html
USER yvpn
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/yvpn-web", "-dir", "/app"]
