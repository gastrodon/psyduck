const parser = require("args-parser");

import iterate from "./iterate";

const ENVIRONMENT_PREFIX: string = "IFUNNY_ETL_";

export const enum ConfigKind {
  Job,
  SourceKind,
  SourceID,
  PerSecond,
  ExitAfter,
  Email,
  Password,
}

const names: Map<ConfigKind, string> = new Map([
  [ConfigKind.Job, "job"],
  [ConfigKind.SourceKind, "source-kind"],
  [ConfigKind.SourceID, "source-id"],
  [ConfigKind.PerSecond, "per-second"],
  [ConfigKind.ExitAfter, "exit-after"],
  [ConfigKind.Email, "email"],
  [ConfigKind.Password, "password"],
]);

const defaults: Map<ConfigKind, any> = new Map([
  [ConfigKind.PerSecond, 20],
]);

const as_env = (key: string): string => {
  return ENVIRONMENT_PREFIX + key.replace("-", "_").toUpperCase();
};

// TODO validate arguments
export const configure = (): Map<ConfigKind, any> => {
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
