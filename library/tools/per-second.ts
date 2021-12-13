import Config from "../types/config";
import { ConfigKind } from "../types/config-kind";

export default (config: Config): number => (
  60_000 / config.get(ConfigKind.PerSecond) as number
);
