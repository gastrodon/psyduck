import configure from "./library/tools/configure";
import Config from "./library/types/config";
import AsyncStream from "./library/types/async-stream";
import AsyncPool from "./library/types/async-pool";
import { StreamConfig } from "./library/types/stream-kind";
import { read, write } from "./library/sources/stream";
import { lookup as stream_lookup } from "./library/types/stream-kind";
import { functions } from "./library/types/transformer-kind";
import TransformerKind from "./library/types/transformer-kind/enum";
import { ConfigKind } from "./library/types/config-kind";
import { async as async_iterate } from "./library/tools/iterate";

type Transformer = (it: any) => any;

interface ETLConfig {
  source: AsyncStream<any>;
  target: AsyncPool<any>;
  transformers: Array<Transformer>;
}

const collect_remote = async (
  config: Config,
  sources: Array<any>,
  count: number,
): Promise<Array<StreamConfig>> => {
  const source_iterators: Array<AsyncStream<string>> = await Promise.all(
    sources
      .map((source: StreamConfig) => read(config, source)),
  );

  const sources_iterated: [Array<string>] = (await Promise.all(
    source_iterators
      .map((it) => async_iterate(it.iterator, count)),
  )) as [Array<string>];

  return sources_iterated.length > 0
    ? sources_iterated[0].map((it: string) => stream_lookup.get(it))
    : [];
};

const etl = async (
  sources: Array<AsyncStream<any>>,
  targets: Array<AsyncPool<any>>,
  transformers: Array<(it: any) => any>,
  count: number,
) => {
  let pushed = 0;

  for (const source of sources) {
    for await (let content of source.iterator) {
      for (const transformer of transformers) {
        content = await transformer(content);
      }

      for (const target of targets) {
        target.push(content);
      }

      pushed++;

      if (count !== 0 && count <= pushed) {
        break;
      }
    }
  }
};

const main = async () => {
  const config = configure();

  const sources = await Promise.all(
    [
      ...await collect_remote(
        config,
        config.get(ConfigKind.SourcesFrom),
        config.get(ConfigKind.SourcesFromCount),
      ),
      ...config.get(ConfigKind.Sources),
    ]
      .map((it: StreamConfig) => read(config, it)),
  );

  const targets = await Promise.all(
    [
      ...await collect_remote(
        config,
        config.get(ConfigKind.TargetsFrom),
        config.get(ConfigKind.TargetsFromCount),
      ),
      ...config.get(ConfigKind.Targets),
    ]
      .map((it: StreamConfig) => write(config, it)),
  );

  const transformers = config
    .get(ConfigKind.Transformers)
    .map((it: TransformerKind) => functions.get(it)!);

  console.log("exiting after", config.get(ConfigKind.ExitAfter));

  await etl(
    sources,
    targets,
    transformers,
    config.get(ConfigKind.ExitAfter),
  );
};

main().catch((it) => {
  throw it;
});
