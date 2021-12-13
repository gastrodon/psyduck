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

  MariadbUsername,
  MariadbPassword,
  MariadbDatabase,
  MariadbTableSchema,

  NoAuth,
  Email,
  Password,

  MariadbHost,
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

  [ConfigKind.MariadbUsername, "mariadb-username"],
  [ConfigKind.MariadbPassword, "mariadb-password"],
  [ConfigKind.MariadbDatabase, "mariadb-database"],
  [ConfigKind.MariadbTableSchema, "mariadb-table-schema"],

  [ConfigKind.NoAuth, "no-auth"],
  [ConfigKind.Email, "email"],
  [ConfigKind.Password, "password"],

  [ConfigKind.MariadbHost, "mariadb-host"],
  [ConfigKind.ScytherHost, "scyther-host"],
  [ConfigKind.FerrothornHost, "ferrothorn-host"],
]);

export const defaults = new Map<ConfigKind, any>([
  [ConfigKind.PerSecond, 20],

  [ConfigKind.NoAuth, "true"],

  [ConfigKind.MariadbHost, "http://localhost"],
  [ConfigKind.ScytherHost, "http://localhost"],
  [ConfigKind.FerrothornHost, "http://localhost"],
]);

export const transformers = new Map<ConfigKind, (it: string) => any>([
  [ConfigKind.Job, (it: string) => job_lookup.get(it)],

  [ConfigKind.KeepFields, (it: string) => it ? it.split(",") : []],

  [ConfigKind.NoAuth, (it: string) => it.toLowerCase() === "true"],

  [ConfigKind.Source, (it: string) => stream_lookup.get(it)],
  [ConfigKind.Target, (it: string) => stream_lookup.get(it)],

  [
    ConfigKind.MariadbTableSchema,
    (it: string) =>
      new Map<string, string>(
        !it ? [] : it
          .split(",")
          .map((it) => [
            it.split(" ")[0],
            it.split(" ").slice(1).join(" "),
          ]),
      ),
  ],

  [ConfigKind.MariadbHost, (it: string) => trim(it, "/")],
  [ConfigKind.ScytherHost, (it: string) => trim(it, "/")],
  [ConfigKind.FerrothornHost, (it: string) => trim(it, "/")],
]);
