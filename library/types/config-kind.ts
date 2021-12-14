import { trim } from "lodash";

import { lookup as stream_lookup } from "./stream-kind";
import {
  lookup as transformer_lookup,
  TransformerKind,
} from "./transformer-kind";

export const enum ConfigKind {
  PerSecond,
  ExitAfter,

  Source,
  Target,
  Transformers,

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
  [ConfigKind.PerSecond, "per-second"],
  [ConfigKind.ExitAfter, "exit-after"],

  [ConfigKind.Source, "source"],
  [ConfigKind.Target, "target"],
  [ConfigKind.Transformers, "transformers"],

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
  [
    ConfigKind.Transformers,
    (it: string) =>
      it
        ? it.split(",").map((it) => transformer_lookup.get(it))
        : [TransformerKind.Nop],
  ],

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
