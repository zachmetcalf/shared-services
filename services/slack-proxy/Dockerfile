# Copyright (c) 2026 Zach Metcalf. All Rights Reserved.

FROM golang:1.22-alpine AS builder

WORKDIR /src

COPY go.mod ./
COPY *.go ./

RUN CGO_ENABLED=0 GOOS=linux go build -o /out/slack-proxy .

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=builder /out/slack-proxy /app/slack-proxy

EXPOSE 8082

ENTRYPOINT ["/app/slack-proxy"]
