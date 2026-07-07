package stdlib

import (
	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/stdlib/consume"
	"github.com/gastrodon/psyduck/stdlib/produce"
	"github.com/gastrodon/psyduck/stdlib/transform"
)

// delimitSpec is the shared stream-framing surface for every transport:
// mutually-exclusive separators plus grouping. It maps onto the continuous
// chunking of the underlying byte stream.
func delimitSpec() []*sdk.Spec {
	return []*sdk.Spec{
		{Name: "sep", Description: "string separator to split/join on", Type: sdk.TypeString, Default: "\n"},
		{Name: "sep-byte", Description: "single byte separator 0..255 (-1 = unset)", Type: sdk.TypeInt, Default: -1},
		{Name: "sep-byte-index", Description: "fixed chunk size in bytes (0 = unset)", Type: sdk.TypeInt, Default: 0},
		{Name: "group", Description: "pieces per emitted message (0/1 = one)", Type: sdk.TypeInt, Default: 0},
	}
}

// encodingSpec is the shared codec surface for the data-shaping transformers.
func encodingSpec(decDefault, encDefault string) []*sdk.Spec {
	return []*sdk.Spec{
		{Name: "decode", Description: "codec chain to decode input (e.g. \"json\", \"gzip|json\")", Type: sdk.TypeString, Default: decDefault},
		{Name: "encode", Description: "codec chain to encode output", Type: sdk.TypeString, Default: encDefault},
		{Name: "on-error", Description: "\"raise\" (default) or \"drop\"", Type: sdk.TypeString, Default: "raise"},
	}
}

func strList() *sdk.Spec { return &sdk.Spec{Type: sdk.TypeString} }

func concat(groups ...[]*sdk.Spec) []*sdk.Spec {
	out := []*sdk.Spec{}
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

func httpSpec() []*sdk.Spec {
	return []*sdk.Spec{
		{Name: "url", Description: "request URL", Type: sdk.TypeString, Required: true},
		{Name: "method", Description: "HTTP method", Type: sdk.TypeString, Default: ""},
		{Name: "headers", Description: "request headers", Type: sdk.TypeMap, ElemType: strList(), Default: map[string]string{}},
		{Name: "body", Description: "static request body (producer)", Type: sdk.TypeString, Default: ""},
		{Name: "query-params", Description: "URL query parameters", Type: sdk.TypeMap, ElemType: strList(), Default: map[string]string{}},
		{Name: "basic-auth", Description: "\"user:pass\" for HTTP Basic auth", Type: sdk.TypeString, Default: ""},
		{Name: "timeout-ms", Description: "request timeout (ms)", Type: sdk.TypeInt, Default: 0},
		{Name: "success-codes", Description: "accepted status codes", Type: sdk.TypeList, ElemType: &sdk.Spec{Type: sdk.TypeInt}, Default: []int{}},
		{Name: "interval-ms", Description: "polling interval (producer, ms)", Type: sdk.TypeInt, Default: 0},
	}
}

func Plugin() sdk.Plugin {
	return sdk.NewInProc("psyduck",
		// ── dev / testing producers ──────────────────────────────────────
		&sdk.Resource{
			Name: "constant", Kinds: sdk.PRODUCER, ProvideProducer: produce.Constant,
			Spec: []*sdk.Spec{
				{Name: "value", Description: "value to emit each iteration", Type: sdk.TypeString, Default: "0"},
				{Name: "stop-after", Description: "stop after n emits (0 = forever)", Type: sdk.TypeInt, Default: 0},
			},
		},
		&sdk.Resource{
			Name: "sequence", Kinds: sdk.PRODUCER, ProvideProducer: produce.Sequence,
			Spec: []*sdk.Spec{
				{Name: "start", Description: "first value", Type: sdk.TypeInt, Default: 0},
				{Name: "step", Description: "increment between values", Type: sdk.TypeInt, Default: 1},
				{Name: "stop-after", Description: "stop after n emits (0 = forever)", Type: sdk.TypeInt, Default: 0},
			},
		},
		&sdk.Resource{
			Name: "generate", Kinds: sdk.PRODUCER, ProvideProducer: produce.Generate,
			Spec: []*sdk.Spec{
				{Name: "values", Description: "literal values to emit in order", Type: sdk.TypeList, ElemType: strList(), Required: true},
				{Name: "loop", Description: "cycle forever", Type: sdk.TypeBool, Default: false},
				{Name: "stop-after", Description: "stop after n emits (0 = forever)", Type: sdk.TypeInt, Default: 0},
			},
		},
		&sdk.Resource{
			Name: "ticker", Kinds: sdk.PRODUCER, ProvideProducer: produce.Ticker,
			Spec: []*sdk.Spec{
				{Name: "interval-ms", Description: "interval between ticks (ms)", Type: sdk.TypeInt, Default: 1000},
				{Name: "format", Description: "unix, unix-ms, or rfc3339", Type: sdk.TypeString, Default: "unix-ms"},
				{Name: "stop-after", Description: "stop after n ticks (0 = forever)", Type: sdk.TypeInt, Default: 0},
			},
		},

		// ── transports ───────────────────────────────────────────────────
		&sdk.Resource{
			Name:            "file",
			Kinds:           sdk.PRODUCER | sdk.CONSUMER,
			ProvideProducer: produce.File,
			ProvideConsumer: consume.File,
			Spec: concat([]*sdk.Spec{
				{Name: "location", Description: "path, \"-\" stdin/stdout, \"--\" stderr, or a socket URI", Type: sdk.TypeString, Required: true},
				{Name: "follow", Description: "tail the file (producer)", Type: sdk.TypeBool, Default: false},
				{Name: "append", Description: "append instead of truncate (consumer)", Type: sdk.TypeBool, Default: false},
				{Name: "create", Description: "create if missing", Type: sdk.TypeBool, Default: false},
			}, delimitSpec()),
		},
		&sdk.Resource{
			Name:            "socket",
			Kinds:           sdk.PRODUCER | sdk.CONSUMER,
			ProvideProducer: produce.Socket,
			ProvideConsumer: consume.Socket,
			Spec: concat([]*sdk.Spec{
				{Name: "location", Description: "tcp://, udp://, or unix:// address", Type: sdk.TypeString, Required: true},
				{Name: "create", Description: "recreate unix socket if present", Type: sdk.TypeBool, Default: false},
			}, delimitSpec()),
		},
		&sdk.Resource{
			Name:            "listen",
			Kinds:           sdk.PRODUCER,
			ProvideProducer: produce.Listen,
			Spec: concat([]*sdk.Spec{
				{Name: "location", Description: "tcp://, unix://, or udp:// address to bind", Type: sdk.TypeString, Required: true},
				{Name: "create", Description: "recreate unix socket if present", Type: sdk.TypeBool, Default: false},
			}, delimitSpec()),
		},
		&sdk.Resource{
			Name:            "request",
			Kinds:           sdk.PRODUCER | sdk.CONSUMER,
			ProvideProducer: produce.Request,
			ProvideConsumer: consume.Request,
			Spec:            httpSpec(),
		},
		&sdk.Resource{
			Name:            "http-listen",
			Kinds:           sdk.PRODUCER,
			ProvideProducer: produce.HTTPListen,
			Spec: []*sdk.Spec{
				{Name: "address", Description: "listen address", Type: sdk.TypeString, Default: ":8080"},
				{Name: "path", Description: "URL path to handle", Type: sdk.TypeString, Default: "/"},
				{Name: "method", Description: "filter by HTTP method (empty = any)", Type: sdk.TypeString, Default: ""},
				{Name: "status", Description: "response status code", Type: sdk.TypeInt, Default: 200},
				{Name: "reply", Description: "response body", Type: sdk.TypeString, Default: ""},
			},
		},

		// ── consumers ────────────────────────────────────────────────────
		&sdk.Resource{Name: "trash", Kinds: sdk.CONSUMER, ProvideConsumer: consume.Trash},

		// ── codec-aware transformers ─────────────────────────────────────
		&sdk.Resource{
			Name: "recode", Kinds: sdk.TRANSFORMER, ProvideTransformer: transform.Recode,
			Spec: encodingSpec("bytes", "bytes"),
		},
		&sdk.Resource{
			Name: "pick", Kinds: sdk.TRANSFORMER, ProvideTransformer: transform.Pick,
			Spec: concat([]*sdk.Spec{
				{Name: "path", Description: "key/index path (discrete selection)", Type: sdk.TypeList, ElemType: strList(), Default: []string{}},
				{Name: "by", Description: "jq expression (continuous selection)", Type: sdk.TypeString, Default: ""},
			}, encodingSpec("json", "")),
		},
		&sdk.Resource{
			Name: "pick-map", Kinds: sdk.TRANSFORMER, ProvideTransformer: transform.PickMap,
			Spec: concat([]*sdk.Spec{
				{Name: "fields", Description: "map of dest key -> source path", Type: sdk.TypeMap, ElemType: &sdk.Spec{Type: sdk.TypeList, ElemType: strList()}, Required: true},
			}, encodingSpec("json", "")),
		},
		&sdk.Resource{
			Name: "set", Kinds: sdk.TRANSFORMER, ProvideTransformer: transform.Set,
			Spec: concat([]*sdk.Spec{
				{Name: "values", Description: "fields to set/overwrite with static values", Type: sdk.TypeMap, ElemType: strList(), Required: true},
			}, encodingSpec("json", "")),
		},
		&sdk.Resource{
			Name: "drop", Kinds: sdk.TRANSFORMER, ProvideTransformer: transform.Drop,
			Spec: concat([]*sdk.Spec{
				{Name: "fields", Description: "top-level fields to remove", Type: sdk.TypeList, ElemType: strList(), Required: true},
			}, encodingSpec("json", "")),
		},
		&sdk.Resource{
			Name: "slice", Kinds: sdk.TRANSFORMER, ProvideTransformer: transform.Slice,
			Spec: concat([]*sdk.Spec{
				{Name: "start", Description: "start index (clamped to bounds; no counting from the end)", Type: sdk.TypeInt, Default: 0},
				{Name: "stop", Description: "stop index (0 or below = through the end)", Type: sdk.TypeInt, Default: 0},
				{Name: "step", Description: "take every step-th element", Type: sdk.TypeInt, Default: 1},
			}, encodingSpec("bytes", "")),
		},
		&sdk.Resource{
			Name: "chunk", Kinds: sdk.TRANSFORMER, ProvideTransformer: transform.Chunk,
			Spec: concat([]*sdk.Spec{
				{Name: "size", Description: "window size", Type: sdk.TypeInt, Required: true},
				{Name: "keep-tail", Description: "keep a short final window", Type: sdk.TypeBool, Default: false},
			}, encodingSpec("bytes", "json")),
		},
		&sdk.Resource{
			Name: "every", Kinds: sdk.TRANSFORMER, ProvideTransformer: transform.Every,
			Spec: concat([]*sdk.Spec{
				{Name: "step", Description: "advance between windows", Type: sdk.TypeInt, Default: 1},
				{Name: "size", Description: "window size", Type: sdk.TypeInt, Default: 1},
			}, encodingSpec("bytes", "json")),
		},
		&sdk.Resource{
			Name: "render", Kinds: sdk.TRANSFORMER, ProvideTransformer: transform.Render,
			Spec: concat([]*sdk.Spec{
				{Name: "engine", Description: "template, printf, or jq", Type: sdk.TypeString, Default: "template"},
				{Name: "format", Description: "template/format/jq-expression string", Type: sdk.TypeString, Required: true},
			}, encodingSpec("json", "bytes")),
		},

		// ── jq escape hatches ────────────────────────────────────────────
		&sdk.Resource{
			Name: "jq", Kinds: sdk.TRANSFORMER, ProvideTransformer: transform.Jq,
			Spec: []*sdk.Spec{{Name: "expression", Description: "jq expression", Type: sdk.TypeString, Required: true}},
		},
		&sdk.Resource{
			Name: "filter", Kinds: sdk.TRANSFORMER, ProvideTransformer: transform.Filter,
			Spec: []*sdk.Spec{{Name: "expression", Description: "jq predicate; drop when false/null", Type: sdk.TypeString, Required: true}},
		},

		// ── keyed transformers ───────────────────────────────────────────
		&sdk.Resource{
			Name: "dedupe", Kinds: sdk.TRANSFORMER, ProvideTransformer: transform.Dedupe,
			Spec: []*sdk.Spec{
				{Name: "by", Description: "jq key expression (continuous)", Type: sdk.TypeString, Default: ""},
				{Name: "path", Description: "key path (discrete)", Type: sdk.TypeList, ElemType: strList(), Default: []string{}},
				{Name: "window", Description: "recent keys to remember", Type: sdk.TypeInt, Default: 0},
			},
		},
		&sdk.Resource{
			Name: "uniq", Kinds: sdk.TRANSFORMER, ProvideTransformer: transform.Uniq,
			Spec: []*sdk.Spec{
				{Name: "by", Description: "jq key expression (continuous)", Type: sdk.TypeString, Default: ""},
				{Name: "path", Description: "key path (discrete)", Type: sdk.TypeList, ElemType: strList(), Default: []string{}},
			},
		},
		&sdk.Resource{
			Name: "batch", Kinds: sdk.TRANSFORMER, ProvideTransformer: transform.Batch,
			Spec: []*sdk.Spec{{Name: "size", Description: "messages per emitted array", Type: sdk.TypeInt, Required: true}},
		},

		// ── flow control ─────────────────────────────────────────────────
		&sdk.Resource{
			Name: "wait", Kinds: sdk.TRANSFORMER, ProvideTransformer: transform.Wait,
			Spec: []*sdk.Spec{{Name: "milliseconds", Description: "sleep per message (ms)", Type: sdk.TypeInt, Required: true}},
		},
		&sdk.Resource{
			Name: "throttle", Kinds: sdk.TRANSFORMER, ProvideTransformer: transform.Throttle,
			Spec: []*sdk.Spec{{Name: "per-second", Description: "max messages per second", Type: sdk.TypeInt, Required: true}},
		},
		&sdk.Resource{
			Name: "head", Kinds: sdk.TRANSFORMER, ProvideTransformer: transform.Head,
			Spec: []*sdk.Spec{{Name: "count", Description: "pass the first n, drop the rest", Type: sdk.TypeInt, Required: true}},
		},
		&sdk.Resource{
			Name: "tail", Kinds: sdk.TRANSFORMER, ProvideTransformer: transform.Tail,
			Spec: []*sdk.Spec{{Name: "skip", Description: "drop the first n, pass the rest", Type: sdk.TypeInt, Required: true}},
		},
		&sdk.Resource{
			Name: "sample", Kinds: sdk.TRANSFORMER, ProvideTransformer: transform.Sample,
			Spec: []*sdk.Spec{{Name: "rate", Description: "keep one in every n", Type: sdk.TypeInt, Required: true}},
		},

		// ── text ─────────────────────────────────────────────────────────
		&sdk.Resource{
			Name: "split", Kinds: sdk.TRANSFORMER, ProvideTransformer: transform.Split,
			Spec: []*sdk.Spec{
				{Name: "delimiter", Description: "split on this (default newline)", Type: sdk.TypeString, Default: "\n"},
				{Name: "decode", Description: "string codec (default utf-8)", Type: sdk.TypeString, Default: "utf-8"},
				{Name: "on-error", Description: "\"raise\" or \"drop\"", Type: sdk.TypeString, Default: "raise"},
			},
		},
		&sdk.Resource{
			Name: "join", Kinds: sdk.TRANSFORMER, ProvideTransformer: transform.Join,
			Spec: []*sdk.Spec{
				{Name: "delimiter", Description: "join with this", Type: sdk.TypeString, Default: ""},
				{Name: "on-error", Description: "\"raise\" or \"drop\"", Type: sdk.TypeString, Default: "raise"},
			},
		},
		&sdk.Resource{
			Name: "replace", Kinds: sdk.TRANSFORMER, ProvideTransformer: transform.Replace,
			Spec: []*sdk.Spec{
				{Name: "old", Description: "substring to replace", Type: sdk.TypeString, Required: true},
				{Name: "new", Description: "replacement", Type: sdk.TypeString, Default: ""},
				{Name: "count", Description: "max replacements (0 = all)", Type: sdk.TypeInt, Default: 0},
				{Name: "decode", Description: "string codec (default utf-8)", Type: sdk.TypeString, Default: "utf-8"},
				{Name: "on-error", Description: "\"raise\" or \"drop\"", Type: sdk.TypeString, Default: "raise"},
			},
		},
		&sdk.Resource{
			Name: "regex", Kinds: sdk.TRANSFORMER, ProvideTransformer: transform.Regex,
			Spec: []*sdk.Spec{
				{Name: "pattern", Description: "regular expression", Type: sdk.TypeString, Required: true},
				{Name: "replacement", Description: "replacement ($1 groups supported)", Type: sdk.TypeString, Default: ""},
				{Name: "decode", Description: "string codec (default utf-8)", Type: sdk.TypeString, Default: "utf-8"},
				{Name: "on-error", Description: "\"raise\" or \"drop\"", Type: sdk.TypeString, Default: "raise"},
			},
		},
		&sdk.Resource{
			Name: "trim", Kinds: sdk.TRANSFORMER, ProvideTransformer: transform.Trim,
			Spec: []*sdk.Spec{
				{Name: "chars", Description: "characters to trim (empty = whitespace)", Type: sdk.TypeString, Default: ""},
				{Name: "side", Description: "both, left, or right", Type: sdk.TypeString, Default: "both"},
				{Name: "decode", Description: "string codec (default utf-8)", Type: sdk.TypeString, Default: "utf-8"},
				{Name: "on-error", Description: "\"raise\" or \"drop\"", Type: sdk.TypeString, Default: "raise"},
			},
		},
		&sdk.Resource{
			Name: "upper", Kinds: sdk.TRANSFORMER, ProvideTransformer: transform.Upper,
			Spec: []*sdk.Spec{
				{Name: "decode", Description: "string codec (default utf-8)", Type: sdk.TypeString, Default: "utf-8"},
				{Name: "on-error", Description: "\"raise\" or \"drop\"", Type: sdk.TypeString, Default: "raise"},
			},
		},
		&sdk.Resource{
			Name: "lower", Kinds: sdk.TRANSFORMER, ProvideTransformer: transform.Lower,
			Spec: []*sdk.Spec{
				{Name: "decode", Description: "string codec (default utf-8)", Type: sdk.TypeString, Default: "utf-8"},
				{Name: "on-error", Description: "\"raise\" or \"drop\"", Type: sdk.TypeString, Default: "raise"},
			},
		},
		&sdk.Resource{
			Name: "hash", Kinds: sdk.TRANSFORMER, ProvideTransformer: transform.Hash,
			Spec: []*sdk.Spec{
				{Name: "algorithm", Description: "sha256 (default), sha512, or md5", Type: sdk.TypeString, Default: "sha256"},
				{Name: "output", Description: "hex (default) or base64", Type: sdk.TypeString, Default: "hex"},
			},
		},

		// ── dev / testing transformers ───────────────────────────────────
		&sdk.Resource{
			Name: "inspect", Kinds: sdk.TRANSFORMER, ProvideTransformer: transform.Inspect,
			Spec: []*sdk.Spec{
				{Name: "prefix", Description: "prepended to each logged line", Type: sdk.TypeString, Default: ""},
				{Name: "output", Description: "stdout (default) or stderr", Type: sdk.TypeString, Default: "stdout"},
			},
		},
		&sdk.Resource{
			Name: "assert", Kinds: sdk.TRANSFORMER, ProvideTransformer: transform.Assert,
			Spec: []*sdk.Spec{
				{Name: "expression", Description: "jq predicate that must hold", Type: sdk.TypeString, Required: true},
				{Name: "message", Description: "error message on failure", Type: sdk.TypeString, Default: ""},
			},
		},
		&sdk.Resource{
			Name: "count", Kinds: sdk.TRANSFORMER, ProvideTransformer: transform.Count,
			Spec: []*sdk.Spec{
				{Name: "every", Description: "emit the count every n messages", Type: sdk.TypeInt, Default: 1},
				{Name: "prefix", Description: "prepended to the emitted count", Type: sdk.TypeString, Default: ""},
			},
		},
	)
}
