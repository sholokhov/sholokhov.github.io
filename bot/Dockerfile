FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /photo-bot .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates && adduser -D -h /app bot
COPY --from=build /photo-bot /usr/local/bin/photo-bot
USER bot
WORKDIR /app

# Mount config at runtime: -v ./config.yaml:/etc/photo-bot/config.yaml
ENTRYPOINT ["photo-bot", "-config", "/etc/photo-bot/config.yaml"]
