const { Client, Post, User } = require("ifunny");

import Config from "../../types/config";
import AsyncStream from "../../types/async-stream";
import AsyncPool from "../../types/async-pool";
import { StreamConfig } from "../../types/stream-kind";
import { ConfigKind } from "../../types/config-kind";

const STREAM_FEED = /^ifunny\/feed\/.+$/;
const STREAM_COMMENTS = /^ifunny\/comments\/.{9}$/;
const STREAM_TAG = /^ifunny\/tag\/.{3,100}$/;
const STREAM_TIMELINE = /^ifunny\/timeline\/.{24}$/;

const get_client = async (config: Config): Promise<any> => {
  if (config.get(ConfigKind.NoAuth)) {
    return new Client();
  }

  let client = Client();
  await client.login({
    email: config.get(ConfigKind.Email),
    password: config.get(ConfigKind.Password),
  });

  return client;
};

const get_feed = async (
  config: Config,
  stream: StreamConfig,
): Promise<AsyncStream<any>> => {
  const client = await get_client(config);

  switch (true) {
    case !!stream.name.match(STREAM_FEED):
      return client[stream.name.split("/")[2]];
    case !!stream.name.match(STREAM_COMMENTS):
      return new Post(stream.name.split("/")[2], { client }).comments;
    case !!stream.name.match(STREAM_TIMELINE):
      return new User(stream.name.split("/")[2], { client }).timeline;
    case !!stream.name.match(STREAM_TAG):
      return client.search_tags(stream.name.split("/")[2]);
  }

  throw `Unknown ifunny resource ${stream.name}`;
};

export const read = async (
  config: Config,
  stream: StreamConfig,
): Promise<AsyncStream<any>> => ({ iterator: await get_feed(config, stream) });

export const write = async (
  config: Config,
  stream: StreamConfig,
): Promise<AsyncPool<any>> => {
  throw "Cannot create an AsyncPool, iFunny feeds are read-only";
};
