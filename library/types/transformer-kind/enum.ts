enum TransformerKind {
  IFunnyContentReference,
  IFunnyUserReference,
  IFunnyCommentSource,
  IFunnyTimelineSource,
  IFunnyCommentArchive,
  IFunnyAuthor,
  IFunnyObject,

  AsMap,
  Jsonify,
  Log,
  Nop,
  Stringify,
}

export default TransformerKind;
