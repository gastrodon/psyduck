const { Client } = require("ifunny");

import Config from "../../types/config";
import AsyncStream from "../../types/async-stream";
import AsyncPool from "../../types/async-pool";
import { StreamConfig } from "../../types/stream-kind";
import { ConfigKind } from "../../types/config-kind";

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

  return {
    "ifunny-feed/collective": client.collective,
    "ifunny-feed/featured": client.featured,
  }[stream.name];
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
