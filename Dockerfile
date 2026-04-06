# syntax=docker/dockerfile:1.7

FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /out/local-api ./cmd/local-api

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=build /out/local-api /usr/local/bin/local-api

ENV DIFFAUDIT_LOCAL_API_HOST=0.0.0.0
ENV DIFFAUDIT_LOCAL_API_PORT=8765

EXPOSE 8765

ENTRYPOINT ["local-api"]
