FROM golang:alpine
WORKDIR /build

RUN apk add --no-cache git openssh gcc musl-dev

ADD . .
RUN go get .
RUN go build -o /psyduck
RUN go env -w CGO_ENABLED=1

ENTRYPOINT ["/psyduck"]