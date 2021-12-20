import TransformerKind from "./enum";

import * as transformer from "../../transformers";

const KEYS_CONTENT_REFERENCE = ["id", "publish_at", "date_create"];
const KEYS_USER_REFERENCE = ["id", "nick", "original_nick"];
const KEYS_COMMENT_REFERENCE = ["id", "cid"];

const KEYS_COMMENT_SNAPSHOT = [
  "cid",
  "date",
  "id",
  "is_edited",
  "is_reply",
  "is_smiled",
  "is_unsmiled",
  "state",
  "text",
];

const KEYS_CONTENT_SNAPSHOT = [
  "id",
  "type",
  "visibility",
  "url",
  "canonical_url",
  "date_create",
  "publish_at",
  "issue_at",
  "is_featured",
  "is_pinned",
  "is_republished",
  "fast_start",
  "can_be_boosted",
];

const KEY_AUTHOR = "creator";
const KEY_OBJECT = "_object_payload";
const KEY_TIMELINE = "timeline"; // TODO add in library

export const names = new Map<TransformerKind, string>([
  [TransformerKind.IFunnyContentReference, "ifunny-content-reference"],
  [TransformerKind.IFunnyUserReference, "ifunny-user-reference"],
  [TransformerKind.IFunnyCommentReference, "ifunny-comment-reference"],
  [TransformerKind.IFunnyCommentSource, "ifunny-comment-source"],
  [TransformerKind.IFunnyTimelineSource, "ifunny-timeline-source"],
  [TransformerKind.IFunnyCommentSnapshot, "ifunny-comment-snapshot"],
  [TransformerKind.IFunnyContentSnapshot, "ifunny-content-snapshot"],
  [TransformerKind.IFunnyLookupContent, "ifunny-lookup-content"],
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
      TransformerKind.IFunnyCommentReference,
      transformer.keep_keys(KEYS_COMMENT_REFERENCE),
    ],
    [
      TransformerKind.IFunnyCommentSource,
      transformer.ifunny.comment_source,
    ],
    [
      TransformerKind.IFunnyTimelineSource,
      transformer.ifunny.timeline_source,
    ],
    [
      TransformerKind.IFunnyCommentSnapshot,
      transformer.keep_keys(KEYS_COMMENT_SNAPSHOT),
    ],
    [
      TransformerKind.IFunnyContentSnapshot,
      transformer.keep_keys(KEYS_CONTENT_SNAPSHOT),
    ],
    [
      TransformerKind.IFunnyLookupContent,
      transformer.ifunny.lookup_content,
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
