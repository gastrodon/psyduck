enum TransformerKind {
  IFunnyContentReference,
  IFunnyUserReference,
  IFunnyCommentReference,
  IFunnyCommentSource,
  IFunnyTimelineSource,
  IFunnyCommentSnapshot,
  IFunnyContentSnapshot,
  IFunnyTagSnapshot,
  IFunnyLookupComment,
  IFunnyLookupContent,
  IFunnyLookupUser,
  IFunnyPartitionTags,
  IFunnyAuthor,
  IFunnyObject,

  AsMap,
  AsMaps,
  Jsonify,
  Log,
  Nop,
  Stringify,
}

export default TransformerKind;
