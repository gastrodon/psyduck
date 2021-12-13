import iterate from "../tools/iterate";

export const enum JobKind {
  QueueLoadFeed,
}

export const names: Map<JobKind, string> = new Map([
  [JobKind.QueueLoadFeed, "queue-load-feed"],
]);

export const lookup: Map<string, JobKind> = new Map(
  iterate(names.entries())
    .map(([kind, name]) => [name, kind]),
);
