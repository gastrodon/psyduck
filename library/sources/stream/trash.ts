import Config from "../../types/config";
import AsyncStream from "../../types/async-stream";
import AsyncPool from "../../types/async-pool";
import { StreamConfig } from "../../types/stream-kind";

async function* iterate_nothing() {
  while (true) yield null;
}

export const read = async (
  config: Config,
  stream: StreamConfig,
): Promise<AsyncStream<any>> => ({
  iterator: await iterate_nothing(),
});

export const write = async (
  config: Config,
  stream: StreamConfig,
): Promise<AsyncPool<any>> => (
  { push: async (_: any) => {} }
);
