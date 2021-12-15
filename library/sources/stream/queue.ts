import axios, { AxiosError } from "axios";

import Config from "../../types/config";
import AsyncStream from "../../types/async-stream";
import AsyncPool from "../../types/async-pool";
import { StreamConfig } from "../../types/stream-kind";
import { ConfigKind } from "../../types/config-kind";

const ensure_queue = (config: Config, stream: StreamConfig) =>
  axios({
    method: "POST",
    data: JSON.stringify({ name: stream.name.split("/")[1] }),
    url: [
      config.get(ConfigKind.ScytherHost),
      "queues",
    ].join("/"),
  }).catch((error: AxiosError) => {
    if (error.response?.data?.error !== "conflict") {
      throw error;
    }
  });

async function* iterate_queue(config: Config, stream: StreamConfig) {
  await ensure_queue(config, stream);

  while (true) {
    try {
      yield (await axios({
        method: "GET",
        url: [
          config.get(ConfigKind.ScytherHost),
          "queues",
          stream.name.split("/")[1],
          "head",
        ].join("/"),
      }))
        .data
        .message;
    } catch (error) {
      if ((error as AxiosError)?.response?.data?.error !== "no_message") {
        throw error;
      }

      return;
    }
  }
}

export const read = async (
  config: Config,
  stream: StreamConfig,
): Promise<AsyncStream<any>> => ({
  iterator: await iterate_queue(config, stream),
});

export const write = async (
  config: Config,
  stream: StreamConfig,
): Promise<AsyncPool<string>> => {
  await ensure_queue(config, stream);

  return {
    push: (data: string) =>
      axios({
        method: "PUT",
        data: data,
        url: [
          config.get(ConfigKind.ScytherHost),
          "queues",
          stream.name.split("/")[1],
        ].join("/"),
      }),
  };
};
