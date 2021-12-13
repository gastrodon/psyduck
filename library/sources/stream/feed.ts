const { Client } = require("ifunny");

import Config from "../../types/config";
import { ConfigKind } from "../../types/config-kind";
import { StreamKind } from "../../types/stream-kind";

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
  kind: StreamKind,
): Promise<any> => {
  const handle = await client(config);

  return (
    new Map<StreamKind, Iterable<any>>([
      [StreamKind.FeedCollective, handle.collective],
      [StreamKind.FeedFeatured, handle.featured],
    ])
  ).get(kind);
};
