import iterate from "../tools/iterate";

export const enum FeedKind {
  Collective,
}

export const names: Map<FeedKind, string> = new Map([
  [FeedKind.Collective, "collective"],
]);

export const lookup: Map<string, FeedKind> = new Map(
  iterate(names.entries())
    .map(([kind, name]) => [name, kind]),
);
