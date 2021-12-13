import Config from "./config";
import iterate from "../tools/iterate";

export const enum StreamKind {
  Feed,
  Queue,
}

export interface StreamConfig {
  kind: StreamKind;
  name: string;
}

export const patterns: Map<StreamKind, RegExp> = new Map([
  [StreamKind.Feed, /^feed\/[\w]+$/],
  [StreamKind.Queue, /^queue\/[\w]+$/],
]);

export const lookup = {
  get: (name: string): StreamConfig =>
    Array.from(patterns.entries())
      .filter(([_, match]) => name.match(match))
      .map(([kind, _]) => ({ kind, name }))[0],
};
