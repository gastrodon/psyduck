FROM golang:alpine

WORKDIR /build
ADD . .

RUN apk add --no-cache git openssh
RUN go get -u ./...
RUN go build -o /psyduck

ENTRYPOINT /psyduck