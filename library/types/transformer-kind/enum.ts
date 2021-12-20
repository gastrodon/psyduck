enum TransformerKind {
  IFunnyContentReference,
  IFunnyUserReference,
  IFunnyCommentReference,
  IFunnyCommentSource,
  IFunnyTimelineSource,
  IFunnyCommentSnapshot,
  IFunnyContentSnapshot,
  IFunnyLookupContent,
  IFunnyAuthor,
  IFunnyObject,

  AsMap,
  Jsonify,
  Log,
  Nop,
  Stringify,
}

export default TransformerKind;
