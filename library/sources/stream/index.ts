import Config from "../../types/config";
import AsyncStream from "../../types/async-stream";
import AsyncPool from "../../types/async-pool";
import { StreamConfig, StreamKind } from "../../types/stream-kind";

type StreamGetter = any; // TODO (_: type): type isn't working idk why

const lookup = new Map<StreamKind, StreamGetter>([
  [StreamKind.IFunnyFeed, require("./ifunny-feed")],
  [StreamKind.Mariadb, require("./mariadb")],
  [StreamKind.Queue, require("./queue")],
  [StreamKind.Trash, require("./trash")],
]);

export const read = (config: Config, stream: StreamConfig): AsyncStream<any> =>
  lookup.get(stream.kind)["read"](config, stream);

export const write = (config: Config, stream: StreamConfig): AsyncPool<any> =>
  lookup.get(stream.kind)["write"](config, stream);
