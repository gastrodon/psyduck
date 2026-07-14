FROM golang:alpine
WORKDIR /build

RUN apk add --no-cache git openssh

ADD . .
RUN go get .
RUN go build -o /psyduck

ENTRYPOINT ["/psyduck"]