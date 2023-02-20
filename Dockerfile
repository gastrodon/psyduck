FROM gastrodon/psyduck-base AS build
FROM debian:stable-slim

VOLUME /plugin
VOLUME /config

COPY --from=build /base/psyduck /psyduck
ENTRYPOINT [ "/psyduck", "-plugin", "/plugin", "-chdir", "/config"]
