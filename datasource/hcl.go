package datasource

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/psyduck-etl/sdk"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/gocty"
)

const (
	nsProduce   = "produce"
	nsConsume   = "consume"
	nsTransform = "transform"
)

// HCL implements Format for HCL-based configuration files.
type HCL struct {
	filename string
	literal  []byte
	initPath string

	file   *hcl.File
	values map[string]cty.Value
}

// NewHCL creates a new HCL format reader.
//   - filename and literal are the HCL config content (as configure.Literal accepts)
//   - initPath is the .psyduck directory where plugin.json and compiled .so files live
func NewHCL(filename string, literal []byte, initPath string) *HCL {
	return &HCL{filename: filename, literal: literal, initPath: initPath}
}

// Plugins reads plugin.json from initPath (written by `psyduck init`) and
// loads the compiled .so plugin binaries listed there.
func (h *HCL) Plugins() ([]*sdk.Plugin, error) {
	data, err := os.ReadFile(filepath.Join(h.initPath, "plugin.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read plugin.json: %w", err)
	}

	var binPaths map[string]string
	if err := json.Unmarshal(data, &binPaths); err != nil {
		return nil, fmt.Errorf("failed to decode plugin.json: %w", err)
	}

	plugins := make([]*sdk.Plugin, 0, len(binPaths))
	for name, soPath := range binPaths {
		p, err := loadPluginBinary(name, soPath)
		if err != nil {
			return nil, err
		}
		plugins = append(plugins, p)
	}

	return plugins, nil
}

// Values parses value {} blocks from the HCL config and returns the
// merged key-value map. Results are cached after the first call.
func (h *HCL) Values() (map[string]cty.Value, error) {
	if h.values != nil {
		return h.values, nil
	}

	file, err := h.hclFile()
	if err != nil {
		return nil, err
	}

	values, err := parseValueBlocks(file.Body)
	if err != nil {
		return nil, err
	}

	h.values = values
	return values, nil
}

// Resources parses produce/consume/transform blocks from the HCL config,
// resolves their attribute values against value.* and env.* namespaces,
// and instantiates each resource via the provided plugins.
func (h *HCL) Resources(plugins []*sdk.Plugin) (*ResourceSources, error) {
	lookup := resourceLookup(plugins)

	valuesCtx, err := h.makeValuesEvalCtx()
	if err != nil {
		return nil, fmt.Errorf("failed to build values eval context: %w", err)
	}

	resources, err := h.parseResources(valuesCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to parse resources: %w", err)
	}
	producers := make(map[string]ProducerSet)
	consumers := make(map[string]ConsumerSet)
	transformers := make(map[string]sdk.Transformer)

	for _, part := range resources.Producers {
		key := resourceName(nsProduce, part)
		r, err := lookup(part.Kind)
		if err != nil {
			return nil, fmt.Errorf("failed to instantiate producer %s: %w", key, err)
		}
		p, err := r.ProvideProducer(hclParser(r.Spec, valuesCtx, part.Options))
		if err != nil {
			return nil, fmt.Errorf("failed to instantiate producer %s: %w", key, err)
		}
		producers[key] = LiteralProducerSet(p)
	}

	for _, part := range resources.Consumers {
		key := resourceName(nsConsume, part)
		r, err := lookup(part.Kind)
		if err != nil {
			return nil, fmt.Errorf("failed to instantiate consumer %s: %w", key, err)
		}
		c, err := r.ProvideConsumer(hclParser(r.Spec, valuesCtx, part.Options))
		if err != nil {
			return nil, fmt.Errorf("failed to instantiate consumer %s: %w", key, err)
		}
		consumers[key] = LiteralConsumerSet(c)
	}

	for _, part := range resources.Transformers {
		key := resourceName(nsTransform, part)
		r, err := lookup(part.Kind)
		if err != nil {
			return nil, fmt.Errorf("failed to instantiate transformer %s: %w", key, err)
		}
		t, err := r.ProvideTransformer(hclParser(r.Spec, valuesCtx, part.Options))
		if err != nil {
			return nil, fmt.Errorf("failed to instantiate transformer %s: %w", key, err)
		}
		transformers[key] = t
	}

	return &ResourceSources{
		Producers:    producers,
		Consumers:    consumers,
		Transformers: transformers,
	}, nil
}

// resourceLookup builds an index from kind name to *sdk.Resource across all plugins.
func resourceLookup(plugins []*sdk.Plugin) func(string) (*sdk.Resource, error) {
	index := make(map[string]*sdk.Resource)
	for _, p := range plugins {
		for _, r := range p.Resources {
			index[r.Name] = r
		}
	}
	return func(kind string) (*sdk.Resource, error) {
		r, ok := index[kind]
		if !ok {
			return nil, &ErrNoValue{Key: kind}
		}
		return r, nil
	}
}

// ---------------------------------------------------------------------------
// Internal: HCL parsing helpers
// ---------------------------------------------------------------------------

// hclPipelinePart mirrors configure.pipelinePart — a single
// produce/consume/transform block with kind and name labels.
type hclPipelinePart struct {
	Kind    string   `hcl:"kind,label" cty:"kind"`
	Name    string   `hcl:"name,label" cty:"kind"`
	Options hcl.Body `hcl:",remain"`
}

// hclPipelineParts collects all top-level resource blocks.
type hclPipelineParts struct {
	Producers    []*hclPipelinePart `hcl:"produce,block"`
	Consumers    []*hclPipelinePart `hcl:"consume,block"`
	Transformers []*hclPipelinePart `hcl:"transform,block"`
}

// resourceName builds the canonical reference key "namespace.kind.name".
func resourceName(namespace string, part *hclPipelinePart) string {
	return strings.Join([]string{namespace, part.Kind, part.Name}, ".")
}

func (h *HCL) hclFile() (*hcl.File, error) {
	if h.file != nil {
		return h.file, nil
	}
	file, diags := hclparse.NewParser().ParseHCL(h.literal, h.filename)
	if diags.HasErrors() {
		return nil, diags
	}
	h.file = file
	return file, nil
}

func (h *HCL) parseResources(evalCtx *hcl.EvalContext) (*hclPipelineParts, error) {
	file, err := h.hclFile()
	if err != nil {
		return nil, err
	}

	parts := new(hclPipelineParts)
	gohcl.DecodeBody(file.Body, evalCtx, parts)
	return parts, nil
}

// buildResourcesCtx builds an HCL eval context that maps resource references
// (produce.kind.name, consume.kind.name, transform.kind.name) to their
// canonical string keys — enabling pipeline blocks to reference resources.
func (h *HCL) buildResourcesCtx(parts *hclPipelineParts) (*hcl.EvalContext, error) {
	produce, err := loadResourceSlice(nsProduce, parts.Producers)
	if err != nil {
		return nil, err
	}

	consume, err := loadResourceSlice(nsConsume, parts.Consumers)
	if err != nil {
		return nil, err
	}

	transform, err := loadResourceSlice(nsTransform, parts.Transformers)
	if err != nil {
		return nil, err
	}

	return &hcl.EvalContext{
		Variables: map[string]cty.Value{
			nsProduce:   produce,
			nsConsume:   consume,
			nsTransform: transform,
		},
	}, nil
}

// loadResourceSlice builds an object value mapping kind -> name -> reference string.
func loadResourceSlice(namespace string, parts []*hclPipelinePart) (cty.Value, error) {
	kinds := make(map[string]map[string]cty.Value)
	for _, part := range parts {
		if _, ok := kinds[part.Kind]; !ok {
			kinds[part.Kind] = make(map[string]cty.Value)
		}
		kinds[part.Kind][part.Name] = cty.StringVal(resourceName(namespace, part))
	}

	if len(kinds) == 0 {
		return cty.EmptyObjectVal, nil
	}

	refs := make(map[string]cty.Value, len(kinds))
	for name, kindMap := range kinds {
		refs[name] = cty.ObjectVal(kindMap)
	}

	return cty.ObjectVal(refs), nil
}

// makeValuesEvalCtx builds an eval context with value.* and env.* namespaces.
func (h *HCL) makeValuesEvalCtx() (*hcl.EvalContext, error) {
	values, err := h.Values()
	if err != nil {
		return nil, err
	}

	var valObj cty.Value
	if len(values) == 0 {
		valObj = cty.EmptyObjectVal
	} else {
		valObj = cty.ObjectVal(values)
	}

	return &hcl.EvalContext{
		Variables: map[string]cty.Value{
			"value": valObj,
			"env": makeMapEnv(),
		},
	}, nil
}

// makeMapEnv builds a cty object from os.Environ.
func makeMapEnv() cty.Value {
	env := os.Environ()
	envMap := make(map[string]cty.Value, len(env))
	for _, kv := range env {
		if idx := strings.Index(kv, "="); idx >= 0 {
			envMap[kv[:idx]] = cty.StringVal(kv[idx+1:])
		}
	}

	if len(envMap) == 0 {
		return cty.EmptyObjectVal
	}

	return cty.ObjectVal(envMap)
}

// ---------------------------------------------------------------------------
// Internal: HCL parser construction + attribute decoding
// ---------------------------------------------------------------------------

// hclParser builds an sdk.Parser that decodes an HCL body into a target
// struct using the resource's spec, the eval context, and cty tagged
// decoding with the "psy" tag.
func hclParser(spec sdk.SpecMap, evalCtx *hcl.EvalContext, body hcl.Body) sdk.Parser {
	return func(target interface{}) error {
		if spec == nil {
			return nil
		}

		content, _, diags := body.PartialContent(makeBodySchema(spec))
		if diags.HasErrors() {
			return diags
		}

		if diags := decodeAttributes(spec, evalCtx, content.Attributes, target); diags.HasErrors() {
			return diags
		}

		return nil
	}
}

// makeBodySchema converts an sdk.SpecMap into an HCL BodySchema suitable for
// PartialContent calls.
func makeBodySchema(specMap sdk.SpecMap) *hcl.BodySchema {
	attributes := make([]hcl.AttributeSchema, 0, len(specMap))
	for _, spec := range specMap {
		attributes = append(attributes, hcl.AttributeSchema{
			Name:     spec.Name,
			Required: spec.Required,
		})
	}

	return &hcl.BodySchema{Attributes: attributes}
}

// decodeAttributes resolves HCL attributes against the spec, validates types,
// applies defaults, and decodes the result into target via gocty with the "psy" tag.
func decodeAttributes(spec sdk.SpecMap, evalCtx *hcl.EvalContext, attributes hcl.Attributes, target interface{}) hcl.Diagnostics {
	diags := hcl.Diagnostics{}

	valuesDecode := make(map[string]cty.Value)
	for name, fieldSpec := range spec {
		fieldValue, diagsValue := getAttributeValue(attributes, name, evalCtx)
		if diagsValue.HasErrors() {
			for _, each := range diagsValue {
				diags = diags.Append(each)
			}
			continue
		}

		if fieldValue.IsNull() {
			if fieldSpec.Required {
				diags = diags.Append(&hcl.Diagnostic{
					Severity:    hcl.DiagError,
					Summary:     "value required",
					Detail:      fmt.Sprintf("a value is required for %s", fieldSpec.Name),
					EvalContext: evalCtx,
				})
			} else {
				valuesDecode[name] = fieldSpec.Default
			}
			continue
		}

		fieldValue = toDynCollection(fieldValue)
		if diagsValidate := validate(fieldValue, fieldSpec); diagsValidate.HasErrors() {
			for _, each := range diagsValidate {
				diags = diags.Append(each)
			}
			continue
		}

		valuesDecode[name] = fieldValue
	}

	if diags.HasErrors() {
		return diags
	}

	if err := gocty.FromCtyValueTagged(cty.ObjectVal(valuesDecode), target, "psy"); err != nil {
		diags = diags.Append(&hcl.Diagnostic{
			Severity:    hcl.DiagError,
			Summary:     "fromCtyValueTagged failed",
			Detail:      fmt.Sprintf("failed to decode cty value: %s", err),
			EvalContext: evalCtx,
		})
	}

	return diags
}

// getAttributeValue evaluates a single attribute expression, returning NilVal
// if the attribute is absent.
func getAttributeValue(attributes hcl.Attributes, name string, evalCtx *hcl.EvalContext) (cty.Value, hcl.Diagnostics) {
	attr, ok := attributes[name]
	if !ok {
		return cty.NilVal, nil
	}
	return attr.Expr.Value(evalCtx)
}

// toDynCollection normalises HCL tuples to lists and objects to maps so that
// the cty type system accepts them for spec validation.
func toDynCollection(value cty.Value) cty.Value {
	switch {
	case value.Type().IsTupleType():
		items := make([]cty.Value, value.LengthInt())
		iter := value.ElementIterator()
		for iter.Next() {
			nextIndex, nextValue := iter.Element()
			index, _ := nextIndex.AsBigFloat().Int64()
			items[int(index)] = toDynCollection(nextValue)
		}
		return cty.ListVal(items)

	case value.Type().IsObjectType():
		items := make(map[string]cty.Value)
		iter := value.ElementIterator()
		for iter.Next() {
			nextKey, nextValue := iter.Element()
			items[nextKey.AsString()] = toDynCollection(nextValue)
		}
		return cty.MapVal(items)

	default:
		return value
	}
}

// ---------------------------------------------------------------------------
// Internal: spec validation
// ---------------------------------------------------------------------------

func validate(value cty.Value, fieldSpec *sdk.Spec) hcl.Diagnostics {
	if value.Type().IsListType() {
		return validateList(value, fieldSpec)
	}

	if value.Type().IsMapType() {
		return validateMap(value, fieldSpec)
	}

	return validatePrimitive(value, fieldSpec)
}

func validatePrimitive(value cty.Value, fieldSpec *sdk.Spec) hcl.Diagnostics {
	if !value.Type().Equals(cty.Type(fieldSpec.Type)) {
		return hcl.Diagnostics{
			&hcl.Diagnostic{
				Severity:   hcl.DiagError,
				Expression: hcl.StaticExpr(value, hcl.Range{}),
				Summary:    "invalid primitive type",
				Detail:     fmt.Sprintf("spec requires a %s, got a value %#v", cty.Type(fieldSpec.Type).FriendlyName(), value),
			},
		}
	}

	return nil
}

func validateList(value cty.Value, fieldSpec *sdk.Spec) hcl.Diagnostics {
	elementType := cty.Type(fieldSpec.Type).ListElementType()
	if elementType == nil {
		panic("list element type is nil")
	}

	iter := value.ElementIterator()
	for iter.Next() {
		nextKey, nextValue := iter.Element()
		index, _ := nextKey.AsBigFloat().Int64()
		childSpec := listItemSpec(fieldSpec, index)
		if diags := validate(nextValue, childSpec); diags != nil {
			return diags
		}
	}

	return nil
}

func validateMap(value cty.Value, fieldSpec *sdk.Spec) hcl.Diagnostics {
	elementType := cty.Type(fieldSpec.Type).MapElementType()
	if elementType == nil {
		panic("map element type is nil")
	}

	iter := value.ElementIterator()
	for iter.Next() {
		nextKey, nextValue := iter.Element()
		childSpec := mapItemSpec(fieldSpec, nextKey.AsString())
		if diags := validate(nextValue, childSpec); diags != nil {
			return diags
		}
	}

	return nil
}

func itemSpec(source *sdk.Spec, key string, baseType *cty.Type) *sdk.Spec {
	name := strings.Join([]string{source.Name, key}, ".")
	if baseType == nil {
		panic(fmt.Sprintf("cannot gather element type of %s", name))
	}

	return &sdk.Spec{
		Name:        name,
		Description: source.Description,
		Required:    source.Required,
		Type:        *baseType,
		Default:     cty.NilVal,
	}
}

func listItemSpec(source *sdk.Spec, index int64) *sdk.Spec {
	return itemSpec(source, strconv.FormatInt(index, 10), cty.Type(source.Type).ListElementType())
}

func mapItemSpec(source *sdk.Spec, key string) *sdk.Spec {
	return itemSpec(source, key, cty.Type(source.Type).MapElementType())
}
