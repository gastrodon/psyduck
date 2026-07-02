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
	)
}
