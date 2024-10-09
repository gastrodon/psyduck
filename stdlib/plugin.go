package stdlib

import (
	"os"

	"github.com/gastrodon/psyduck/stdlib/consume"
	"github.com/gastrodon/psyduck/stdlib/produce"
	"github.com/gastrodon/psyduck/stdlib/transform"
	"github.com/psyduck-etl/sdk"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
)

func Plugin() *sdk.Plugin {
	return &sdk.Plugin{
		Name: "psyduck",
		Resources: []*sdk.Resource{
			{
				Name:            "constant",
				Kinds:           sdk.PRODUCER,
				ProvideProducer: produce.Constant,
				Spec: []*sdk.Spec{
					{
						Name:        "value",
						Description: "constant value to produce",
						Type:        cty.String,
						Default:     cty.StringVal("0"),
					},
					{
						Name:        "stop-after",
						Description: "stop after n iterations",
						Type:        cty.Number,
						Default:     cty.NumberIntVal(0),
					},
				},
			},
			{
				Name:            "trash",
				Kinds:           sdk.CONSUMER,
				ProvideConsumer: consume.Trash,
			},
			{
				Name:               "inspect",
				Kinds:              sdk.TRANSFORMER,
				ProvideTransformer: transform.Inspect,
				Spec: []*sdk.Spec{
					{
						Name:        "be-string",
						Description: "should the data bytes should be a string",
						Type:        cty.Bool,
						Default:     cty.BoolVal(true),
					},
				},
			},
			{
				Name:               "snippet",
				Kinds:              sdk.TRANSFORMER,
				ProvideTransformer: transform.Snippet,
				Spec: []*sdk.Spec{
					{
						Name:        "fields",
						Description: "fields to take a snippet of",
						Type:        cty.List(cty.String),
						Required:    true,
					},
				},
			},
			{
				Name:  "sprintf",
				Kinds: sdk.TRANSFORMER,
				Spec: []*sdk.Spec{
					{
						Name:        "format",
						Description: "String to format values into",
						Type:        cty.String,
						Required:    true,
					},
					{
						Name:        "encoding",
						Description: "How the formatted value will be encoded",
						Type:        cty.String,
						Required:    false,
						Default:     cty.StringVal("bytes"),
					},
				},
				ProvideTransformer: transform.Sprintf,
			},
			{
				Name:               "transpose",
				Kinds:              sdk.TRANSFORMER,
				ProvideTransformer: transform.Transpose,
				Spec: []*sdk.Spec{
					{
						Name:        "fields",
						Description: "fields to transpose, mapping of target -> source",
						Type:        cty.Map(cty.List(cty.String)),
						Required:    true,
					},
				},
			},
			{
				Name:               "wait",
				Kinds:              sdk.TRANSFORMER,
				ProvideTransformer: transform.Wait,
				Spec: []*sdk.Spec{
					{
						Name:        "milliseconds",
						Description: "duratin to wait in ms",
						Type:        cty.Number,
						Required:    true,
					},
				},
			},
			{
				Name:               "zoom",
				Kinds:              sdk.TRANSFORMER,
				ProvideTransformer: transform.Zoom,
				Spec: []*sdk.Spec{
					{
						Name:        "field",
						Description: "field to zoom into",
						Type:        cty.String,
						Required:    true,
					},
				},
			},
			{
				Name:  "increment",
				Kinds: sdk.PRODUCER,
				Spec: []*sdk.Spec{
					{
						Name:        "stop-after",
						Description: "stop after n iterations",
						Type:        cty.Number,
						Default:     cty.NumberIntVal(0),
					},
				},
				ProvideProducer: produce.Increment,
			},
		},
		Functions: map[string]function.Function{
			"env": function.New(&function.Spec{
				Description: "Read an environment variable",
				Params: []function.Parameter{{
					Name:        "env",
					Description: "Environment variable to read",
					Type:        cty.String,
				}},
				Type: func(args []cty.Value) (cty.Type, error) {
					return cty.String, nil
				},
				Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
					return cty.StringVal(os.Getenv(args[0].AsString())), nil
				},
			}),
		},
	}
}
