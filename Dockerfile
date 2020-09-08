FROM golang:1.15-alpine as builder

ENV CGO_ENABLED=0

WORKDIR /build
COPY go.mod go.sum *.go ./

RUN go build -o codenotify


FROM alpine:3.12

# hadolint ignore=DL3018
# RUN apk add --no-cache ca-certificates

COPY --from=builder /build/codenotify /usr/local/bin/

ENTRYPOINT ["codenotify"]
