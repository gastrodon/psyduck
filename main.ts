import configure from "./library/tools/configure";
import AsyncStream from "./library/types/async-stream";
import AsyncPool from "./library/types/async-pool";
import { read, write } from "./library/sources/stream";
import { ConfigKind } from "./library/types/config-kind";

type Transformer = (it: any) => any;

interface ETLConfig {
  source: AsyncStream<any>;
  target: AsyncPool<any>;
  transformers: Array<Transformer>;
}

const etl = async (
  source: AsyncStream<any>,
  target: AsyncPool<any>,
  transformers: Array<(it: any) => any>,
) => {
  console.log(`starting etl`);

  for await (let content of source.iterator) {
    for (const transformer of transformers) {
      content = await transformer(content);
    }

    target.push(content);
  }
};

const main = async () => {
  const config = configure();

  const source = await read(config, config.get(ConfigKind.Source));
  const target = await write(config, config.get(ConfigKind.Target));
  const transformers = config.get(ConfigKind.Transformers);

  etl(source, target, transformers);
};

main().catch((it) => {
  throw it;
});
