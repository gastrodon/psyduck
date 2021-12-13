import Config from "../../types/config";
import { StreamConfig, StreamKind } from "../../types/stream-kind";

type StreamGetter = any; // TODO (_: type): type isn't working idk why

const lookup = new Map<StreamKind, StreamGetter>([
  [StreamKind.Feed, require("./feed").default],
  [StreamKind.Queue, require("./queue").attach],
]);

export default (config: Config, stream: StreamConfig): any =>
  lookup.get(stream.kind)(config, stream);
