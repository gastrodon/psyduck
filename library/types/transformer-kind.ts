import * as transformer from "../transformers";
import { sync as iterate } from "../tools/iterate";
import TransformerKind from "./transformer-kind/enum";
import * as ifunny from "./transformer-kind/ifunny";

export const names: Map<TransformerKind, string> = new Map([
  ...ifunny.names,

  [TransformerKind.AsMap, "as-map"],
  [TransformerKind.AsMaps, "as-maps"],
  [TransformerKind.Jsonify, "jsonify"],
  [TransformerKind.Log, "log"],
  [TransformerKind.Nop, "nop"],
  [TransformerKind.Stringify, "stringify"],
]);

export const functions: Map<TransformerKind, (it: any) => any> = new Map(
  [
    ...ifunny.functions,

    [TransformerKind.AsMap, transformer.as_map],
    [TransformerKind.AsMaps, transformer.as_maps],
    [TransformerKind.Jsonify, transformer.jsonify],
    [TransformerKind.Log, transformer.log],
    [TransformerKind.Nop, transformer.nop],
    [TransformerKind.Stringify, transformer.stringify],
  ],
);

export const lookup: Map<string, TransformerKind> = new Map(
  iterate(names.entries())
    .map(([kind, name]: [TransformerKind, string]) => [name, kind]),
);
