enum TransformerKind {
  IFunnyContentReference,
  IFunnyUserReference,
  IFunnyCommentReference,
  IFunnyCommentSource,
  IFunnyTimelineSource,
  IFunnyCommentArchive,
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
