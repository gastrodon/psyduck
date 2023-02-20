FROM golang:alpine AS build

WORKDIR /build
ADD . .

RUN go get -u ./...
RUN go build -o /build/psyduck

FROM alpine:latest

VOLUME /plugin
VOLUME /config

COPY --from=build /build/psyduck /psyduck
ENTRYPOINT [ "/psyduck", "-plugin", "/plugin", "-chdir", "/config"]
