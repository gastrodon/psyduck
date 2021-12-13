import { v4 } from "uuid";
import { trim } from "lodash";

import { lookup as stream_lookup } from "./stream-kind";
import { lookup as job_lookup } from "./job-kind";

export const enum ConfigKind {
  Job,
  PerSecond,
  ExitAfter,

  Source,
  Target,
  KeepFields,

  NoAuth,
  Email,
  Password,
  ScytherHost,
  FerrothornHost,
}

export const names = new Map<ConfigKind, string>([
  [ConfigKind.Job, "job"],
  [ConfigKind.PerSecond, "per-second"],
  [ConfigKind.ExitAfter, "exit-after"],

  [ConfigKind.Source, "source"],
  [ConfigKind.Target, "target"],
  [ConfigKind.KeepFields, "keep-fields"],

  [ConfigKind.NoAuth, "no-auth"],
  [ConfigKind.Email, "email"],
  [ConfigKind.Password, "password"],

  [ConfigKind.ScytherHost, "scyther-host"],
  [ConfigKind.FerrothornHost, "ferrothorn-host"],
]);

export const defaults = new Map<ConfigKind, any>([
  [ConfigKind.PerSecond, 20],

  [ConfigKind.NoAuth, "true"],

  [ConfigKind.ScytherHost, "http://localhost"],
  [ConfigKind.FerrothornHost, "http://localhost"],
]);

export const transformers = new Map<ConfigKind, (it: string) => any>([
  [ConfigKind.Job, (it: string) => job_lookup.get(it)],

  [ConfigKind.KeepFields, (it: string) => it ? it.split(",") : []],

  [ConfigKind.NoAuth, (it: string) => it.toLowerCase() === "true"],

  [ConfigKind.Source, (it: string) => stream_lookup.get(it)],
  [ConfigKind.Target, (it: string) => stream_lookup.get(it)],

  [ConfigKind.ScytherHost, (it: string) => trim(it, "/")],
  [ConfigKind.FerrothornHost, (it: string) => trim(it, "/")],
]);
