# Multi-stage: the builder compiles a static binary; the final image only
# keeps that binary plus the runtime tools `psyduck init` shells out to
# (go for `go build`, git+openssh for `git clone` / `git@` sources).
# Neither the source tree nor the build/module caches survive into the
# final image.
FROM golang:alpine AS build
WORKDIR /src

# Prime the module cache first so unrelated source edits don't invalidate
# the dependency-download layer.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# CGO off: pure-Go net/os/user, no libc link, portable static binary.
# -trimpath strips builder-local paths; -s -w drop symbol + DWARF.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/psyduck .

FROM golang:alpine
RUN apk add --no-cache git openssh
COPY --from=build /out/psyduck /usr/local/bin/psyduck
WORKDIR /work
ENTRYPOINT ["psyduck"]
