# Сборка статического бинарника: SQLite здесь на чистом Go, cgo не нужен.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/span .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata wget && adduser -D -u 10001 span
WORKDIR /app
COPY --from=build /out/span /app/span
RUN mkdir -p /data && chown -R span:span /data /app
USER span
ENV SPAN_DB=/data/span.db SPAN_ADDR=:8080
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
  CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1
CMD ["/app/span"]
