package stdlib

import (
	"github.com/gastrodon/psyduck/stdlib/consume"
	"github.com/gastrodon/psyduck/stdlib/produce"
	"github.com/gastrodon/psyduck/stdlib/transform"
	"github.com/psyduck-etl/sdk"
	"github.com/zclconf/go-cty/cty"
)

func Plugin() *sdk.Plugin {
	return &sdk.Plugin{
		Name: "psyduck",
		Resources: []*sdk.Resource{
			{
				Name:            "constant",
				Kinds:           sdk.PRODUCER,
				ProvideProducer: produce.Constant,
				Spec: sdk.SpecMap{
					"value": &sdk.Spec{
						Name:        "value",
						Description: "constant value to produce",
						Type:        cty.String,
						Default:     cty.StringVal("0"),
					},
					"stop-after": &sdk.Spec{
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
				Spec: sdk.SpecMap{
					"be-string": &sdk.Spec{
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
				Spec: sdk.SpecMap{
					"fields": &sdk.Spec{
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
				Spec: sdk.SpecMap{
					"format": &sdk.Spec{
						Name:        "format",
						Description: "String to format values into",
						Type:        cty.String,
						Required:    true,
					},
					"encoding": &sdk.Spec{
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
				Spec: sdk.SpecMap{
					"fields": &sdk.Spec{
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
				Spec: sdk.SpecMap{
					"milliseconds": &sdk.Spec{
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
				Spec: sdk.SpecMap{
					"field": &sdk.Spec{
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
				Spec: sdk.SpecMap{
					"stop-after": &sdk.Spec{
						Name:        "stop-after",
						Description: "stop after n iterations",
						Type:        cty.Number,
						Default:     cty.NumberIntVal(0),
					},
				},
				ProvideProducer: produce.Increment,
			},
		},
	}
}
