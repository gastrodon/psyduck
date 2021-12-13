// For loading a stream of data onto a queue to process later

import Config from "../../types/config";
import { ConfigKind } from "../../types/config-kind";
import { attach } from "../../tools/queue";

export default async (config: Config) => {
  const target = attach(config, config.get(ConfigKind.QueueTarget));

  console.log(await target.head());
};
