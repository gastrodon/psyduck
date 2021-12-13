import iterate from "../tools/iterate";
import Config from "./config";

export const enum StreamKind {
  Feed,
  Mariadb,
  Queue,
}

export interface StreamConfig {
  kind: StreamKind;
  name: string;
}

export const patterns: Map<StreamKind, RegExp> = new Map([
  [StreamKind.Feed, /^feed\/[\w]+$/],
  [StreamKind.Mariadb, /^mariadb\/[\w]+$/],
  [StreamKind.Queue, /^queue\/[\w]+$/],
]);

export const lookup = {
  get: (name: string): StreamConfig =>
    Array.from(patterns.entries())
      .filter(([_, match]) => name.match(match))
      .map(([kind, _]) => ({ kind, name }))[0],
};
