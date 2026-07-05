package transform

import (
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash"
	"regexp"
	"strings"

	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/stdlib/data"
)

// Text transformers operate in the string domain. `decode` chooses how bytes
// become text — "utf-8" (default, rune-aware, rejects invalid bytes), "ascii",
// "latin1", or a byte-codec prefix like "base64|utf-8". This is why strings are
// not codec-free: the decode step is where encoding and validity are decided,
// and `on-error` governs what happens to bytes that do not decode.

// textString decodes the message to a Go string per the decode spec.
func textString(in []byte, decode string) (string, error) {
	if decode == "" {
		decode = "utf-8"
	}
	v, err := data.Decode(in, decode)
	if err != nil {
		return "", err
	}
	return v.String(), nil
}

// stringTransformer wraps a string→(Value) op with decode + on-error handling.
// A nil onError defaults to data.Raise.
func stringTransformer(decode string, onError data.OnError, op func(string) (data.Value, error)) sdk.Transformer {
	if decode == "" {
		decode = "utf-8"
	}
	if onError == nil {
		onError = data.Raise
	}
	fail := func(err error) ([]byte, error) { return nil, onError(err) }
	return func(in []byte) ([]byte, error) {
		s, err := textString(in, decode)
		if err != nil {
			return fail(err)
		}
		out, err := op(s)
		if err != nil {
			return fail(err)
		}
		if out == nil {
			return nil, nil
		}
		return out.Bytes(), nil
	}
}

type splitConfig struct {
	Delimiter string `psy:"delimiter"`
	Decode    string `psy:"decode"`
	OnError   string `psy:"on-error"`
}

// Split cuts the text on a delimiter (default newline) and emits a JSON array
// of the pieces.
func Split(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(splitConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	delim := config.Delimiter
	if delim == "" {
		delim = "\n"
	}
	onError, err := data.ParseOnError(config.OnError)
	if err != nil {
		return nil, err
	}
	return stringTransformer(config.Decode, onError, func(s string) (data.Value, error) {
		parts := strings.Split(s, delim)
		list := make(data.List, len(parts))
		for i, p := range parts {
			list[i] = data.Str(p)
		}
		return list, nil
	}), nil
}

type joinConfig struct {
	Delimiter string `psy:"delimiter"`
	OnError   string `psy:"on-error"`
}

// Join concatenates a JSON array of strings into a single string with the given
// delimiter.
func Join(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(joinConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	onError, err := data.ParseOnError(config.OnError)
	if err != nil {
		return nil, err
	}
	return func(in []byte) ([]byte, error) {
		v, err := data.Decode(in, "json")
		if err != nil {
			return nil, onError(err)
		}
		list, ok := v.(data.List)
		if !ok {
			return nil, onError(fmt.Errorf("join: want a list, got %s", v.Kind()))
		}
		parts := make([]string, len(list))
		for i, e := range list {
			parts[i] = e.String()
		}
		return []byte(strings.Join(parts, config.Delimiter)), nil
	}, nil
}

type replaceConfig struct {
	Old     string `psy:"old"`
	New     string `psy:"new"`
	Count   int    `psy:"count"`
	Decode  string `psy:"decode"`
	OnError string `psy:"on-error"`
}

// Replace substitutes occurrences of old with new. A count of 0 replaces all.
func Replace(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(replaceConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	count := config.Count
	if count == 0 {
		count = -1
	}
	onError, err := data.ParseOnError(config.OnError)
	if err != nil {
		return nil, err
	}
	return stringTransformer(config.Decode, onError, func(s string) (data.Value, error) {
		return data.Str(strings.Replace(s, config.Old, config.New, count)), nil
	}), nil
}

type regexConfig struct {
	Pattern     string `psy:"pattern"`
	Replacement string `psy:"replacement"`
	Decode      string `psy:"decode"`
	OnError     string `psy:"on-error"`
}

// Regex applies a regular-expression substitution, supporting $1 capture-group
// references in the replacement.
func Regex(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(regexConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	re, err := regexp.Compile(config.Pattern)
	if err != nil {
		return nil, fmt.Errorf("regex: compile %q: %w", config.Pattern, err)
	}
	onError, err := data.ParseOnError(config.OnError)
	if err != nil {
		return nil, err
	}
	return stringTransformer(config.Decode, onError, func(s string) (data.Value, error) {
		return data.Str(re.ReplaceAllString(s, config.Replacement)), nil
	}), nil
}

type trimConfig struct {
	Chars   string `psy:"chars"`
	Side    string `psy:"side"`
	Decode  string `psy:"decode"`
	OnError string `psy:"on-error"`
}

// Trim removes leading/trailing characters (whitespace by default). Side is
// "both" (default), "left", or "right".
func Trim(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(trimConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	onError, err := data.ParseOnError(config.OnError)
	if err != nil {
		return nil, err
	}
	return stringTransformer(config.Decode, onError, func(s string) (data.Value, error) {
		return data.Str(trimSide(s, config.Chars, config.Side)), nil
	}), nil
}

func trimSide(s, chars, side string) string {
	if chars == "" {
		switch side {
		case "left":
			return strings.TrimLeftFunc(s, isSpace)
		case "right":
			return strings.TrimRightFunc(s, isSpace)
		default:
			return strings.TrimSpace(s)
		}
	}
	switch side {
	case "left":
		return strings.TrimLeft(s, chars)
	case "right":
		return strings.TrimRight(s, chars)
	default:
		return strings.Trim(s, chars)
	}
}

func isSpace(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' }

type caseConfig struct {
	Decode  string `psy:"decode"`
	OnError string `psy:"on-error"`
}

// Upper uppercases the text.
func Upper(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(caseConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	onError, err := data.ParseOnError(config.OnError)
	if err != nil {
		return nil, err
	}
	return stringTransformer(config.Decode, onError, func(s string) (data.Value, error) {
		return data.Str(strings.ToUpper(s)), nil
	}), nil
}

// Lower lowercases the text.
func Lower(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(caseConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	onError, err := data.ParseOnError(config.OnError)
	if err != nil {
		return nil, err
	}
	return stringTransformer(config.Decode, onError, func(s string) (data.Value, error) {
		return data.Str(strings.ToLower(s)), nil
	}), nil
}

type hashConfig struct {
	Algorithm string `psy:"algorithm"`
	Output    string `psy:"output"`
}

// Hash replaces the message with a digest of its bytes. Algorithm is one of
// sha256 (default), sha512, or md5; output is "hex" (default) or "base64".
func Hash(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(hashConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	var newHash func() hash.Hash
	switch config.Algorithm {
	case "", "sha256":
		newHash = sha256.New
	case "sha512":
		newHash = sha512.New
	case "md5":
		newHash = md5.New
	default:
		return nil, fmt.Errorf("hash: unknown algorithm %q", config.Algorithm)
	}

	return func(in []byte) ([]byte, error) {
		h := newHash()
		h.Write(in)
		sum := h.Sum(nil)
		switch config.Output {
		case "", "hex":
			return []byte(hex.EncodeToString(sum)), nil
		case "base64":
			return []byte(base64.StdEncoding.EncodeToString(sum)), nil
		default:
			return nil, fmt.Errorf("hash: unknown output %q", config.Output)
		}
	}, nil
}
