FROM node:alpine as builder
WORKDIR /app

COPY ./package.json .
RUN yarn install \
    --also dev \
    --no-lockfile \
    --non-interactive

COPY ./tsconfig.json ./main.ts .
COPY ./library ./library
RUN NODE_OPTIONS=--openssl-legacy-provider \
    yarn run ncc build \
    --target es2021 \
    -o /out

FROM node:alpine
WORKDIR /app

COPY --from=builder /out/index.js .

ENTRYPOINT node ./index.js
