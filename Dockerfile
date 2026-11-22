FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY proto ./proto
RUN CGO_ENABLED=0 go build -tags nomsgpack -o /out/feed-api ./cmd/feed-api && \
    CGO_ENABLED=0 go build -o /out/interaction-service ./cmd/interaction-service

FROM alpine:3.23 AS base
RUN apk add --no-cache ca-certificates tzdata && addgroup -S app && adduser -S -G app app
WORKDIR /app
FROM base AS interaction
COPY --from=build /out/interaction-service /app/interaction-service
USER app
ENTRYPOINT ["/app/interaction-service"]
FROM base AS feed
COPY --from=build /out/feed-api /app/feed-api
COPY --from=web /src/web/dist ./web/dist
COPY media ./media
RUN mkdir -p /app/data/uploads && chown -R app:app /app/data
USER app
ENTRYPOINT ["/app/feed-api"]
