import iterate from "../tools/iterate";

export const enum StreamKind {
  FeedCollective,
  FeedFeatured,
}

export const names: Map<StreamKind, string> = new Map([
  [StreamKind.FeedCollective, "feed/collective"],
  [StreamKind.FeedFeatured, "feed/featured"],
]);

export const lookup: Map<string, StreamKind> = new Map(
  iterate(names.entries())
    .map(([kind, name]) => [name, kind]),
);
