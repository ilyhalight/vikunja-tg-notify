FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod ./
COPY *.go ./

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/vikunja-tg-notify \
    .

FROM alpine:3.22

RUN apk add --no-cache ca-certificates \
    && addgroup -S app \
    && adduser -S -G app app

COPY --from=build /out/vikunja-tg-notify /usr/local/bin/vikunja-tg-notify

USER app
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/vikunja-tg-notify"]
