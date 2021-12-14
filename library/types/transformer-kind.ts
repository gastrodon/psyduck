import * as transformer from "../transformers";
import iterate from "../tools/iterate";

const KEYS_IFUNNY_CONTENT_REFERENCE = ["id", "publish_at", "date_create"];
const KEYS_IFUNNY_USER_REFERENCE = ["id", "nick", "original_nick"];

const KEY_IFUNNY_OBJECT = "_object_payload";
const KEY_IFUNNY_AUTHOR = "creator";

export const enum TransformerKind {
  Nop,
  AsMap,
  Log,
  Jsonify,
  Stringify,

  IFunnyContentReference,
  IFunnyUserReference,

  IFunnyObject,
  IFunnyAuthor,
}

export const names: Map<TransformerKind, string> = new Map([
  [TransformerKind.Nop, "nop"],
  [TransformerKind.AsMap, "as-map"],
  [TransformerKind.Log, "log"],
  [TransformerKind.Jsonify, "jsonify"],
  [TransformerKind.Stringify, "stringify"],

  [TransformerKind.IFunnyContentReference, "ifunny-content-reference"],
  [TransformerKind.IFunnyUserReference, "ifunny-user-reference"],

  [TransformerKind.IFunnyObject, "ifunny-object"],
  [TransformerKind.IFunnyAuthor, "ifunny-author"],
]);

export const lookup: Map<string, TransformerKind> = new Map(
  iterate(names.entries())
    .map(([kind, name]: [TransformerKind, string]) => [name, kind]),
);

export const functions: Map<TransformerKind, (it: any) => any> = new Map(
  [
    [TransformerKind.Nop, transformer.nop],
    [TransformerKind.AsMap, transformer.as_map],
    [TransformerKind.Log, transformer.log],
    [TransformerKind.Jsonify, transformer.jsonify],
    [TransformerKind.Stringify, transformer.stringify],

    [
      TransformerKind.IFunnyContentReference,
      transformer.keep_keys(KEYS_IFUNNY_CONTENT_REFERENCE),
    ],
    [
      TransformerKind.IFunnyUserReference,
      transformer.keep_keys(KEYS_IFUNNY_USER_REFERENCE),
    ],

    [
      TransformerKind.IFunnyAuthor,
      transformer.zoom_key(KEY_IFUNNY_AUTHOR),
    ],
    [
      TransformerKind.IFunnyObject,
      transformer.zoom_key(KEY_IFUNNY_OBJECT),
    ],
  ],
);
