// For loading a stream of data onto a queue to process later

import { ConfigKind } from "../../types/config-kind";
import { attach } from "../../tools/queue";

export default async (config: Map<ConfigKind, any>) => {
  const target = attach(config, config.get(ConfigKind.QueueTarget));

  console.log(await target.head());
};
