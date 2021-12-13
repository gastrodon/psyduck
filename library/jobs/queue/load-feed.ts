// For loading a stream of data onto a queue to process later

import sleep from "../../tools/sleep";
import per_second from "../../tools/per-second";
import Config from "../../types/config";
import { read, write } from "../../sources/stream";
import { ConfigKind } from "../../types/config-kind";

export default async (config: Config) => {
  const source = await read(config, config.get(ConfigKind.Source));
  const target = await write(config, config.get(ConfigKind.Target));
  const keep = config.get(ConfigKind.KeepFields) ?? null;

  for await (const content of source.iterator) {
    const keys = keep.length ? keep : Object.keys(content._object_payload);
    const data = Object.fromEntries(keys.map(
      (it: string) => [it, content._object_payload[it]],
    ));

    target.push(JSON.stringify(data));
    await sleep(per_second(config));
  }
};
