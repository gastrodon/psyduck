const parser = require("args-parser");

import iterate from "./iterate";
import { ConfigKind, defaults, names } from "../types/config-kind";

const ENVIRONMENT_PREFIX: string = "IFUNNY_ETL_";

const as_env = (key: string): string => {
  return ENVIRONMENT_PREFIX + key.replace("-", "_").toUpperCase();
};

// TODO validate arguments
export default (): Map<ConfigKind, any> => {
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
    )),
  );
};
