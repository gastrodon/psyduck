// For loading a stream of data onto a queue to process later

import stream from "../../sources/stream";
import Config from "../../types/config";
import { ConfigKind } from "../../types/config-kind";
import { attach } from "../../tools/queue";
import sleep from "../../tools/sleep";
import per_second from "../../tools/per-second";

export default async (config: Config) => {
  const source = await stream(config, config.get(ConfigKind.StreamSource));
  const target = await attach(config, config.get(ConfigKind.QueueTarget));
  const keep = config.get(ConfigKind.KeepFields) ?? null;

  for await (const content of source) {
    const keys = keep.length ? keep : Object.keys(content._object_payload);
    const data = Object.fromEntries(keys.map(
      (it: string) => [it, content._object_payload[it]],
    ));

    target.push(JSON.stringify(data));
    await sleep(per_second(config));
  }
};
