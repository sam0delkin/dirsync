FROM golang AS debug

WORKDIR /alpha

FROM golang:alpine AS builder

RUN mkdir "/build"

ADD . /build

WORKDIR /build

RUN go build -o dirsync
RUN chmod +x dirsync

FROM alpine

COPY --from=builder /build/dirsync /usr/local/bin/dirsync

ENTRYPOINT dirsync
