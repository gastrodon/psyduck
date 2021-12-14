import TransformerKind from "./enum";

import * as transformer from "../../transformers";

const KEYS_CONTENT_REFERENCE = ["id", "publish_at", "date_create"];
const KEYS_USER_REFERENCE = ["id", "nick", "original_nick"];

const KEY_AUTHOR = "creator";
const KEY_OBJECT = "_object_payload";
const KEY_TIMELINE = "timeline"; // TODO add in library

export const names = new Map<TransformerKind, string>([
  [TransformerKind.IFunnyContentReference, "ifunny-content-reference"],
  [TransformerKind.IFunnyUserReference, "ifunny-user-reference"],
  [TransformerKind.IFunnyTimelineReference, "ifunny-timeline-reference"],
  [TransformerKind.IFunnyAuthor, "ifunny-author"],
  [TransformerKind.IFunnyObject, "ifunny-object"],
]);

export const functions: Map<TransformerKind, (it: any) => any> = new Map(
  [
    [
      TransformerKind.IFunnyContentReference,
      transformer.keep_keys(KEYS_CONTENT_REFERENCE),
    ],
    [
      TransformerKind.IFunnyUserReference,
      transformer.keep_keys(KEYS_USER_REFERENCE),
    ],
    [
      TransformerKind.IFunnyTimelineReference,
      transformer.ifunny.timeline_source,
    ],
    [
      TransformerKind.IFunnyAuthor,
      transformer.zoom_key(KEY_AUTHOR),
    ],
    [
      TransformerKind.IFunnyObject,
      transformer.zoom_key(KEY_OBJECT),
    ],
  ],
);
