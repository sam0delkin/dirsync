FROM golang

RUN mkdir "/build"

ADD . /build

WORKDIR /build

RUN go build -o dirsync
RUN chmod +x dirsync

RUN cp dirsync /usr/local/bin/dirsync

ENTRYPOINT dirsync
