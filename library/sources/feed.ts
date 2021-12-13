import { Client } from "ifunny";

import Config from "../types/config";
import { ConfigKind } from "../types/config-kind";
import { FeedKind, names } from "../types/feed-kind";

const client_from = (config: Config): Client => {
  if (config.get(ConfigKind.NoAuth)) {
    return new Client();
  }

  let client = Client();
  await client.login({
    email: config.get(Config.Email),
    password: config.get(Config.Password),
  });

  return client;
};

export default async (config: Config, feed: FeedKind) => {
  return await client_from(config)[names.get(feed)];
};
