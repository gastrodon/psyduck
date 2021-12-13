export const enum ConfigKind {
  Job,
  SourceKind,
  SourceID,
  PerSecond,
  ExitAfter,
  Email,
  Password,
}

export const names: Map<ConfigKind, string> = new Map([
  [ConfigKind.Job, "job"],
  [ConfigKind.SourceKind, "source-kind"],
  [ConfigKind.SourceID, "source-id"],
  [ConfigKind.PerSecond, "per-second"],
  [ConfigKind.ExitAfter, "exit-after"],
  [ConfigKind.Email, "email"],
  [ConfigKind.Password, "password"],
]);

export const defaults: Map<ConfigKind, any> = new Map([
  [ConfigKind.PerSecond, 20],
]);
