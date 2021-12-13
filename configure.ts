const parser = require("args-parser");

const ENVIRONMENT_PREFIX: string = "IFUNNY_ETL_";

export const enum Config {
  Job,
  SourceKind,
  SourceID,
  PerSecond,
  ExitAfter,
  Email,
  Password,
}

const names: Map<Config, string> = new Map([
  [Config.Job, "job"],
  [Config.SourceKind, "source-kind"],
  [Config.SourceID, "source-id"],
  [Config.PerSecond, "per-second"],
  [Config.ExitAfter, "exit-after"],
  [Config.Email, "email"],
  [Config.Password, "password"],
]);

const defaults: Map<Config, any> = new Map([
  [Config.PerSecond, 20],
]);

const iterate = <T>(iterable: Iterable<T>): Array<T> => {
  let buffer = new Array();

  for (let it of iterable) {
    buffer.push(it);
  }

  return buffer;
};

const as_env = (key: string): string => {
  return ENVIRONMENT_PREFIX + key.replace("-", "_").toUpperCase();
};

// TODO validate arguments
export const configure = (): Map<Config, any> => {
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
