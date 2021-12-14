import { sync as iterate } from "../tools/iterate";
import Config from "./config";

export const enum StreamKind {
  IFunnyFeed,
  Mariadb,
  Queue,
  Trash,
}

export interface StreamConfig {
  kind: StreamKind;
  name: string;
}

export const patterns: Map<StreamKind, RegExp> = new Map([
  [StreamKind.IFunnyFeed, /^ifunny-feed\/[\w]+$/],
  [StreamKind.Mariadb, /^mariadb\/[\w_-]+$/],
  [StreamKind.Queue, /^queue\/[\w_]+$/],
  [StreamKind.Trash, /^trash$/],
]);

export const lookup = {
  get: (name: string): StreamConfig =>
    Array.from(patterns.entries())
      .filter(([_, match]) => name.match(match))
      .map(([kind, _]) => ({ kind, name }))[0],
};
