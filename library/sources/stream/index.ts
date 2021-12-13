import Config from "../../types/config";
import { StreamKind } from "../../types/stream-kind";

type StreamGetter = any; // TODO (_: type): type isn't working idk why

const lookup = new Map<StreamKind, StreamGetter>([
  [StreamKind.FeedCollective, require("./feed").default],
  [StreamKind.FeedFeatured, require("./feed").default],
]);

export default (config: Config, kind: StreamKind): AsyncGenerator<any> => {
  return lookup.get(kind)(config, kind);
};
