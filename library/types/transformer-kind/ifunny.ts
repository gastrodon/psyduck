import TransformerKind from "./enum";

import * as transformer from "../../transformers";

const KEYS_CONTENT_REFERENCE = ["id", "publish_at", "date_create"];
const KEYS_USER_REFERENCE = ["id", "nick", "original_nick"];
const KEYS_COMMENT_REFERENCE = ["id", "cid"];

const KEYS_COMMENT_SNAPSHOT = [
  "id",
  "cid",
  "state",
  "text",
  "date",
  "is_reply",
  "is_edited",
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

const KEYS_TAG_SNAPSHOT = [
  "id",
  "tags",
];

const KEY_AUTHOR = "creator";
const KEY_OBJECT = "_object_payload";
const KEY_TIMELINE = "timeline";

export const names = new Map<TransformerKind, string>([
  [TransformerKind.IFunnyContentReference, "ifunny-content-reference"],
  [TransformerKind.IFunnyUserReference, "ifunny-user-reference"],
  [TransformerKind.IFunnyCommentReference, "ifunny-comment-reference"],
  [TransformerKind.IFunnyCommentSource, "ifunny-comment-source"],
  [TransformerKind.IFunnyTimelineSource, "ifunny-timeline-source"],
  [TransformerKind.IFunnyCommentSnapshot, "ifunny-comment-snapshot"],
  [TransformerKind.IFunnyContentSnapshot, "ifunny-content-snapshot"],
  [TransformerKind.IFunnyTagSnapshot, "ifunny-tag-snapshot"],
  [TransformerKind.IFunnyLookupComment, "ifunny-lookup-comment"],
  [TransformerKind.IFunnyLookupContent, "ifunny-lookup-content"],
  [TransformerKind.IFunnyLookupUser, "ifunny-lookup-user"],
  [TransformerKind.IFunnyPartitionTags, "ifunny-partition-tags"],
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
      TransformerKind.IFunnyTagSnapshot,
      transformer.keep_keys(KEYS_TAG_SNAPSHOT),
    ],
    [
      TransformerKind.IFunnyLookupComment,
      transformer.ifunny.lookup_comment,
    ],
    [
      TransformerKind.IFunnyLookupContent,
      transformer.ifunny.lookup_content,
    ],
    [
      TransformerKind.IFunnyLookupUser,
      transformer.ifunny.lookup_user,
    ],
    [
      TransformerKind.IFunnyPartitionTags,
      transformer.partition("tags"),
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
