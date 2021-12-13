import { trim } from "lodash";
import { v4 } from "uuid";

export const enum ConfigKind {
  Job,
  PerSecond,
  ExitAfter,

  QueueSource,
  QueueTarget,
  StreamSource,
  KeepFields,

  Email,
  Password,
  ScytherHost,
  FerrothornHost,
}

export const names = new Map<ConfigKind, string>([
  [ConfigKind.Job, "job"],
  [ConfigKind.PerSecond, "per-second"],
  [ConfigKind.ExitAfter, "exit-after"],

  [ConfigKind.QueueSource, "queue-source"],
  [ConfigKind.QueueTarget, "queue-target"],
  [ConfigKind.StreamSource, "stream-source"],
  [ConfigKind.KeepFields, "keep-fields"],

  [ConfigKind.Email, "email"],
  [ConfigKind.Password, "password"],

  [ConfigKind.ScytherHost, "scyther-host"],
  [ConfigKind.FerrothornHost, "ferrothorn-host"],
]);

export const defaults = new Map<ConfigKind, any>([
  [ConfigKind.PerSecond, 20],

  [ConfigKind.QueueSource, v4()],
  [ConfigKind.QueueTarget, v4()],

  [ConfigKind.ScytherHost, "http://localhost"],
  [ConfigKind.FerrothornHost, "http://localhost"],
]);

export const transformers = new Map<ConfigKind, (it: string) => any>([
  [ConfigKind.KeepFields, (it: string) => (it ?? "").split(",")],
  [ConfigKind.ScytherHost, (it: string) => trim(it, "/")],
  [ConfigKind.FerrothornHost, (it: string) => trim(it, "/")],
]);
