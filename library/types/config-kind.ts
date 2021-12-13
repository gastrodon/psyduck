export const enum ConfigKind {
  Job,
  PerSecond,
  ExitAfter,
  Email,
  Password,
  QueueSource,
  QueueTarget,
  StreamSource,
  KeepFields,
}

export const names = new Map<ConfigKind, string>([
  [ConfigKind.Job, "job"],
  [ConfigKind.PerSecond, "per-second"],
  [ConfigKind.ExitAfter, "exit-after"],
  [ConfigKind.Email, "email"],
  [ConfigKind.Password, "password"],
  [ConfigKind.QueueSource, "queue-source"],
  [ConfigKind.QueueTarget, "queue-target"],
  [ConfigKind.StreamSource, "stream-source"],
  [ConfigKind.KeepFields, "keep-fields"],
]);

export const defaults = new Map<ConfigKind, any>([
  [ConfigKind.PerSecond, 20],
]);

export const transformers = new Map<ConfigKind, (it: string) => any>([
  [ConfigKind.KeepFields, (it: string) => (it ?? "").split(",")],
]);
