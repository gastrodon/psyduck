import iterate from "../tools/iterate";

export const enum JobKind {
  QueueLoadStream,
}

export const names: Map<JobKind, string> = new Map([
  [JobKind.QueueLoadStream, "queue-load-stream"],
]);

export const lookup: Map<string, JobKind> = new Map(
  iterate(names.entries())
    .map(([kind, name]) => [name, kind]),
);
