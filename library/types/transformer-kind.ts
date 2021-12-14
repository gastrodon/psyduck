import * as transformer from "../transformers";
import iterate from "../tools/iterate";

const KEYS_DATABASE_IFUNNY_CONTENT = ["id", "published_at", "created_at"];
const KEYS_QUEUE_IFUNNY_CONTENT = ["id", "published_at", "created_at"];

export const enum TransformerKind {
  Nop,
  DatabaseIFunnyContent,
  QueueIFunnyContent,
}

export const names: Map<TransformerKind, string> = new Map([
  [TransformerKind.Nop, "nop"],
  [TransformerKind.DatabaseIFunnyContent, "database-ifunny-content"],
  [TransformerKind.QueueIFunnyContent, "queue-ifunny-content"],
]);

export const lookup: Map<string, TransformerKind> = new Map(
  iterate(names.entries())
    .map(([kind, name]: [TransformerKind, string]) => [name, kind]),
);

export const functions: Map<TransformerKind, (it: any) => any> = new Map(
  [
    [TransformerKind.Nop, transformer.nop],

    [
      TransformerKind.DatabaseIFunnyContent,
      transformer.keep_keys(KEYS_DATABASE_IFUNNY_CONTENT),
    ],

    [
      TransformerKind.QueueIFunnyContent,
      transformer.keep_keys(KEYS_QUEUE_IFUNNY_CONTENT),
    ],
  ],
);
