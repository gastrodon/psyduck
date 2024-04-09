FROM golang:alpine AS build

WORKDIR /base
ADD . .

RUN go get -u ./...
RUN go build -o /base/psyduck

FROM alpine:latest 
RUN apk add --no-cache git openssh
COPY --from=build /base/psyduck /psyduck

ENTRYPOINT /psyduck