FROM node:alpine
WORKDIR /app

ADD package.json package.json
ADD yarn.lock yarn.lock
RUN yarn install

ADD . .
ENTRYPOINT yarn start
