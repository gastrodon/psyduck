const parser = require("args-parser");

import Config from "../types/config";

import iterate from "./iterate";
import {
  ConfigKind,
  defaults,
  names,
  transformers,
} from "../types/config-kind";

const ENVIRONMENT_PREFIX: string = "IFUNNY_ETL_";

const as_env = (key: string): string => {
  return ENVIRONMENT_PREFIX + key.replace("-", "_").toUpperCase();
};

const noop = (it: any): any => it;

// TODO validate arguments
export default (): Config => {
  const args = parser(process.argv);

  return new Map(
    iterate(names.entries()).map((
      [kind, name],
    ) => (
      [
        kind,
        args[name] ??
          process.env[as_env(name)] ??
          defaults.get(kind) ??
          null,
      ]
    )).map(([kind, value]) => [kind, (transformers.get(kind) ?? noop)(value)]),
  );
};
