package stdlib

import (
	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/stdlib/consume"
	"github.com/gastrodon/psyduck/stdlib/produce"
	"github.com/gastrodon/psyduck/stdlib/transform"
)

func Plugin() sdk.Plugin {
	return sdk.NewInProc("psyduck",
		&sdk.Resource{
			Name:            "constant",
			Kinds:           sdk.PRODUCER,
			ProvideProducer: produce.Constant,
			Spec: []*sdk.Spec{
				{
					Name:        "value",
					Description: "constant value to produce",
					Type:        sdk.TypeString,
					Default:     "0",
				},
				{
					Name:        "stop-after",
					Description: "stop after n iterations",
					Type:        sdk.TypeInt,
					Default:     0,
				},
			},
		},
		&sdk.Resource{
			Name:            "trash",
			Kinds:           sdk.CONSUMER,
			ProvideConsumer: consume.Trash,
		},
		&sdk.Resource{
			Name:               "inspect",
			Kinds:              sdk.TRANSFORMER,
			ProvideTransformer: transform.Inspect,
			Spec: []*sdk.Spec{
				{
					Name:        "be-string",
					Description: "should the data bytes should be a string",
					Type:        sdk.TypeBool,
					Default:     true,
				},
			},
		},
		&sdk.Resource{
			Name:               "snippet",
			Kinds:              sdk.TRANSFORMER,
			ProvideTransformer: transform.Snippet,
			Spec: []*sdk.Spec{
				{
					Name:        "fields",
					Description: "fields to take a snippet of",
					Type:        sdk.TypeList,
					ElemType:    &sdk.Spec{Type: sdk.TypeString},
					Required:    true,
				},
			},
		},
		&sdk.Resource{
			Name:               "sprintf",
			Kinds:              sdk.TRANSFORMER,
			ProvideTransformer: transform.Sprintf,
			Spec: []*sdk.Spec{
				{
					Name:        "format",
					Description: "String to format values into",
					Type:        sdk.TypeString,
					Required:    true,
				},
				{
					Name:        "encoding",
					Description: "How the formatted value will be encoded",
					Type:        sdk.TypeString,
					Default:     "bytes",
				},
			},
		},
		&sdk.Resource{
			Name:               "transpose",
			Kinds:              sdk.TRANSFORMER,
			ProvideTransformer: transform.Transpose,
			Spec: []*sdk.Spec{
				{
					Name:        "fields",
					Description: "fields to transpose, mapping of target -> source",
					Type:        sdk.TypeMap,
					ElemType:    &sdk.Spec{Type: sdk.TypeList, ElemType: &sdk.Spec{Type: sdk.TypeString}},
					Required:    true,
				},
			},
		},
		&sdk.Resource{
			Name:               "wait",
			Kinds:              sdk.TRANSFORMER,
			ProvideTransformer: transform.Wait,
			Spec: []*sdk.Spec{
				{
					Name:        "milliseconds",
					Description: "duration to wait in ms",
					Type:        sdk.TypeInt,
					Required:    true,
				},
			},
		},
		&sdk.Resource{
			Name:               "zoom",
			Kinds:              sdk.TRANSFORMER,
			ProvideTransformer: transform.Zoom,
			Spec: []*sdk.Spec{
				{
					Name:        "field",
					Description: "field to zoom into",
					Type:        sdk.TypeString,
					Required:    true,
				},
			},
		},
		&sdk.Resource{
			Name:            "increment",
			Kinds:           sdk.PRODUCER,
			ProvideProducer: produce.Increment,
			Spec: []*sdk.Spec{
				{
					Name:        "stop-after",
					Description: "stop after n iterations",
					Type:        sdk.TypeInt,
					Default:     0,
				},
			},
		},

		// ── new producers ────────────────────────────────────────────────
		&sdk.Resource{
			Name:            "file-lines",
			Kinds:           sdk.PRODUCER,
			ProvideProducer: produce.FileLines,
			Spec: []*sdk.Spec{
				{Name: "path", Description: "path to the file", Type: sdk.TypeString, Required: true},
				{Name: "follow", Description: "tail the file (like tail -f)", Type: sdk.TypeBool, Default: false},
				{Name: "skip-empty", Description: "skip empty lines", Type: sdk.TypeBool, Default: false},
			},
		},
		&sdk.Resource{
			Name:            "file-bytes",
			Kinds:           sdk.PRODUCER,
			ProvideProducer: produce.FileBytes,
			Spec: []*sdk.Spec{
				{Name: "path", Description: "path to the file", Type: sdk.TypeString, Required: true},
				{Name: "chunk-size", Description: "bytes per message", Type: sdk.TypeInt, Default: 4096},
			},
		},
		&sdk.Resource{
			Name:            "gz-lines",
			Kinds:           sdk.PRODUCER,
			ProvideProducer: produce.GzLines,
			Spec: []*sdk.Spec{
				{Name: "path", Description: "path to the .gz file", Type: sdk.TypeString, Required: true},
				{Name: "skip-empty", Description: "skip empty lines", Type: sdk.TypeBool, Default: false},
			},
		},
		&sdk.Resource{
			Name:            "zip-lines",
			Kinds:           sdk.PRODUCER,
			ProvideProducer: produce.ZipLines,
			Spec: []*sdk.Spec{
				{Name: "path", Description: "path to the .zip file", Type: sdk.TypeString, Required: true},
				{Name: "match", Description: "glob pattern to select entries (default: *)", Type: sdk.TypeString, Default: "*"},
				{Name: "skip-empty", Description: "skip empty lines", Type: sdk.TypeBool, Default: false},
			},
		},
		&sdk.Resource{
			Name:            "tar-lines",
			Kinds:           sdk.PRODUCER,
			ProvideProducer: produce.TarLines,
			Spec: []*sdk.Spec{
				{Name: "path", Description: "path to the tar archive", Type: sdk.TypeString, Required: true},
				{Name: "match", Description: "glob pattern to select entries (default: *)", Type: sdk.TypeString, Default: "*"},
				{Name: "compression", Description: "none, gz, bz2, or auto (default: auto)", Type: sdk.TypeString, Default: "auto"},
				{Name: "skip-empty", Description: "skip empty lines", Type: sdk.TypeBool, Default: false},
			},
		},
		&sdk.Resource{
			Name:            "stdin",
			Kinds:           sdk.PRODUCER,
			ProvideProducer: produce.Stdin,
			Spec: []*sdk.Spec{
				{Name: "skip-empty", Description: "skip empty lines", Type: sdk.TypeBool, Default: false},
			},
		},
		&sdk.Resource{
			Name:            "cmd",
			Kinds:           sdk.PRODUCER,
			ProvideProducer: produce.Cmd,
			Spec: []*sdk.Spec{
				{Name: "command", Description: "executable to run", Type: sdk.TypeString, Required: true},
				{Name: "args", Description: "arguments", Type: sdk.TypeList, ElemType: &sdk.Spec{Type: sdk.TypeString}, Default: []string{}},
				{Name: "split-lines", Description: "emit one message per line (default: true)", Type: sdk.TypeBool, Default: true},
			},
		},
		&sdk.Resource{
			Name:            "http-poll",
			Kinds:           sdk.PRODUCER,
			ProvideProducer: produce.HttpPoll,
			Spec: []*sdk.Spec{
				{Name: "url", Description: "URL to poll", Type: sdk.TypeString, Required: true},
				{Name: "method", Description: "HTTP method (default: GET)", Type: sdk.TypeString, Default: "GET"},
				{Name: "headers", Description: "request headers", Type: sdk.TypeMap, ElemType: &sdk.Spec{Type: sdk.TypeString}, Default: map[string]string{}},
				{Name: "body", Description: "request body", Type: sdk.TypeString, Default: ""},
				{Name: "interval-ms", Description: "milliseconds between polls (0 = no sleep; use per-minute for rate limiting)", Type: sdk.TypeInt, Default: 0},
			},
		},
		&sdk.Resource{
			Name:            "http-server",
			Kinds:           sdk.PRODUCER,
			ProvideProducer: produce.HttpServer,
			Spec: []*sdk.Spec{
				{Name: "address", Description: "listen address (default: :8080)", Type: sdk.TypeString, Default: ":8080"},
				{Name: "path", Description: "HTTP path to handle (default: /)", Type: sdk.TypeString, Default: "/"},
			},
		},
		&sdk.Resource{
			Name:            "tcp-read",
			Kinds:           sdk.PRODUCER,
			ProvideProducer: produce.TcpRead,
			Spec: []*sdk.Spec{
				{Name: "address", Description: "host:port to connect to", Type: sdk.TypeString, Required: true},
				{Name: "skip-empty", Description: "skip empty lines", Type: sdk.TypeBool, Default: false},
			},
		},
		&sdk.Resource{
			Name:            "tcp-listen",
			Kinds:           sdk.PRODUCER,
			ProvideProducer: produce.TcpListen,
			Spec: []*sdk.Spec{
				{Name: "address", Description: "host:port to listen on", Type: sdk.TypeString, Required: true},
				{Name: "skip-empty", Description: "skip empty lines", Type: sdk.TypeBool, Default: false},
			},
		},
		&sdk.Resource{
			Name:            "udp-listen",
			Kinds:           sdk.PRODUCER,
			ProvideProducer: produce.UdpListen,
			Spec: []*sdk.Spec{
				{Name: "address", Description: "host:port to listen on", Type: sdk.TypeString, Required: true},
			},
		},

		// ── new consumers ────────────────────────────────────────────────
		&sdk.Resource{
			Name:            "file-write",
			Kinds:           sdk.CONSUMER,
			ProvideConsumer: consume.FileWrite,
			Spec: []*sdk.Spec{
				{Name: "path", Description: "destination file path", Type: sdk.TypeString, Required: true},
				{Name: "append", Description: "append to existing file (default: false = truncate)", Type: sdk.TypeBool, Default: false},
				{Name: "delimiter", Description: "written after each message (default: newline)", Type: sdk.TypeString, Default: "\n"},
			},
		},
		&sdk.Resource{
			Name:            "gz-write",
			Kinds:           sdk.CONSUMER,
			ProvideConsumer: consume.GzWrite,
			Spec: []*sdk.Spec{
				{Name: "path", Description: "destination .gz file path", Type: sdk.TypeString, Required: true},
				{Name: "level", Description: "compression level 1-9 (0 = default)", Type: sdk.TypeInt, Default: 0},
				{Name: "delimiter", Description: "written after each message (default: newline)", Type: sdk.TypeString, Default: "\n"},
			},
		},
		&sdk.Resource{
			Name:            "tar-write",
			Kinds:           sdk.CONSUMER,
			ProvideConsumer: consume.TarWrite,
			Spec: []*sdk.Spec{
				{Name: "path", Description: "destination tar archive path", Type: sdk.TypeString, Required: true},
				{Name: "compression", Description: "none or gz (default: none)", Type: sdk.TypeString, Default: "none"},
				{Name: "entry-name-field", Description: "JSON field to use as the tar entry filename", Type: sdk.TypeString, Default: ""},
			},
		},
		&sdk.Resource{
			Name:            "stdout",
			Kinds:           sdk.CONSUMER,
			ProvideConsumer: consume.Stdout,
			Spec: []*sdk.Spec{
				{Name: "delimiter", Description: "written after each message (default: newline)", Type: sdk.TypeString, Default: "\n"},
			},
		},
		&sdk.Resource{
			Name:            "stderr",
			Kinds:           sdk.CONSUMER,
			ProvideConsumer: consume.Stderr,
			Spec: []*sdk.Spec{
				{Name: "delimiter", Description: "written after each message (default: newline)", Type: sdk.TypeString, Default: "\n"},
			},
		},
		&sdk.Resource{
			Name:            "cmd",
			Kinds:           sdk.CONSUMER,
			ProvideConsumer: consume.Cmd,
			Spec: []*sdk.Spec{
				{Name: "command", Description: "executable to run per message", Type: sdk.TypeString, Required: true},
				{Name: "args", Description: "arguments", Type: sdk.TypeList, ElemType: &sdk.Spec{Type: sdk.TypeString}, Default: []string{}},
				{Name: "delimiter", Description: "appended after message on stdin (default: newline)", Type: sdk.TypeString, Default: "\n"},
			},
		},
		&sdk.Resource{
			Name:            "http-post",
			Kinds:           sdk.CONSUMER,
			ProvideConsumer: consume.HttpPost,
			Spec: []*sdk.Spec{
				{Name: "url", Description: "URL to POST to", Type: sdk.TypeString, Required: true},
				{Name: "method", Description: "HTTP method (default: POST)", Type: sdk.TypeString, Default: "POST"},
				{Name: "headers", Description: "request headers", Type: sdk.TypeMap, ElemType: &sdk.Spec{Type: sdk.TypeString}, Default: map[string]string{}},
				{Name: "content-type", Description: "Content-Type header (default: application/octet-stream)", Type: sdk.TypeString, Default: "application/octet-stream"},
			},
		},
		&sdk.Resource{
			Name:            "tcp-write",
			Kinds:           sdk.CONSUMER,
			ProvideConsumer: consume.TcpWrite,
			Spec: []*sdk.Spec{
				{Name: "address", Description: "host:port to connect to", Type: sdk.TypeString, Required: true},
				{Name: "delimiter", Description: "written after each message (default: newline)", Type: sdk.TypeString, Default: "\n"},
			},
		},
		&sdk.Resource{
			Name:            "udp-write",
			Kinds:           sdk.CONSUMER,
			ProvideConsumer: consume.UdpWrite,
			Spec: []*sdk.Spec{
				{Name: "address", Description: "host:port to send to", Type: sdk.TypeString, Required: true},
			},
		},

		// ── new transformers ─────────────────────────────────────────────
		&sdk.Resource{
			Name:               "gzip-compress",
			Kinds:              sdk.TRANSFORMER,
			ProvideTransformer: transform.GzipCompress,
			Spec: []*sdk.Spec{
				{Name: "level", Description: "compression level 1-9 (0 = default)", Type: sdk.TypeInt, Default: 0},
			},
		},
		&sdk.Resource{
			Name:               "gzip-decompress",
			Kinds:              sdk.TRANSFORMER,
			ProvideTransformer: transform.GzipDecompress,
		},
		&sdk.Resource{
			Name:               "base64-encode",
			Kinds:              sdk.TRANSFORMER,
			ProvideTransformer: transform.Base64Encode,
			Spec: []*sdk.Spec{
				{Name: "encoding", Description: "std, url, raw-std, or raw-url (default: std)", Type: sdk.TypeString, Default: "std"},
			},
		},
		&sdk.Resource{
			Name:               "base64-decode",
			Kinds:              sdk.TRANSFORMER,
			ProvideTransformer: transform.Base64Decode,
			Spec: []*sdk.Spec{
				{Name: "encoding", Description: "std, url, raw-std, or raw-url (default: std)", Type: sdk.TypeString, Default: "std"},
			},
		},
		&sdk.Resource{
			Name:               "filter",
			Kinds:              sdk.TRANSFORMER,
			ProvideTransformer: transform.Filter,
			Spec: []*sdk.Spec{
				{Name: "expression", Description: "jq expression; message is dropped when the result is false or null", Type: sdk.TypeString, Required: true},
			},
		},
		&sdk.Resource{
			Name:               "jq",
			Kinds:              sdk.TRANSFORMER,
			ProvideTransformer: transform.Jq,
			Spec: []*sdk.Spec{
				{Name: "expression", Description: "jq expression to apply to the message", Type: sdk.TypeString, Required: true},
			},
		},
		&sdk.Resource{
			Name:               "template",
			Kinds:              sdk.TRANSFORMER,
			ProvideTransformer: transform.Template,
			Spec: []*sdk.Spec{
				{Name: "format", Description: "Go text/template string; JSON messages are decoded so fields are accessible as {{.field}}", Type: sdk.TypeString, Required: true},
			},
		},
		&sdk.Resource{
			Name:               "set-field",
			Kinds:              sdk.TRANSFORMER,
			ProvideTransformer: transform.SetField,
			Spec: []*sdk.Spec{
				{Name: "field", Description: "JSON field name to create or overwrite", Type: sdk.TypeString, Required: true},
				{Name: "expression", Description: "jq expression whose result becomes the field value (e.g. \"now | todate\" for ISO timestamp)", Type: sdk.TypeString, Required: true},
			},
		},
		&sdk.Resource{
			Name:               "dedupe",
			Kinds:              sdk.TRANSFORMER,
			ProvideTransformer: transform.Dedupe,
			Spec: []*sdk.Spec{
				{Name: "by", Description: "jq expression to compute the deduplication key (default: . = whole message)", Type: sdk.TypeString, Default: "."},
				{Name: "window", Description: "number of recent keys to remember (default: 10000)", Type: sdk.TypeInt, Default: 10000},
			},
		},
	)
}
