FROM gastrodon/psyduck-base AS build
FROM alpine:latest

VOLUME /plugin
VOLUME /config

COPY --from=build /base/psyduck /psyduck
ENTRYPOINT [ "/psyduck", "-plugin", "/plugin", "-chdir", "/config"]
