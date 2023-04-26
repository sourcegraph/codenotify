FROM golang:1.20-alpine as builder

WORKDIR /build
COPY . .

RUN go build -o codenotify ./cmd/codenotify

FROM alpine:3

# hadolint ignore=DL3018
RUN apk add --no-cache git

COPY --from=builder /build/codenotify /usr/local/bin/

ENTRYPOINT ["codenotify"]
