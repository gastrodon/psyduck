const { Client } = require("ifunny");

import Config from "../../types/config";
import { ConfigKind } from "../../types/config-kind";
import { StreamConfig } from "../../types/stream-kind";

const client = async (config: Config): Promise<any> => {
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

export default async (
  config: Config,
  stream: StreamConfig,
): Promise<any> => {
  const handle = await client(config);

  return {
    "feed/collective": handle.collective,
    "feed/featured": handle.featured,
  }[stream.name];
};
