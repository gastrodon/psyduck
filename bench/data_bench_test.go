package bench

import (
	"testing"

	"github.com/gastrodon/psyduck/stdlib/data"
)

// BenchmarkDecode measures data.Decode -- the entry point every codec-aware
// transformer and transport goes through to turn raw bytes into the Value
// tree. This is the single most-called function in the engine's hot path.
func BenchmarkDecode(b *testing.B) {
	cases := []struct {
		name string
		spec string
		in   []byte
	}{
		{"json/small-37B", "json", payloadSmall},
		{"json/medium-230B", "json", payloadMedium},
		{"json/large-200rec", "json", payloadLarge},
		{"bytes/medium", "bytes", payloadMedium},
		{"utf-8/medium", "utf-8", payloadMedium},
		{"base64/medium", "base64", payloadMediumBase64},
		{"gzip/medium", "gzip", payloadMediumGzipped},
		{"gzip|json/medium", "gzip|json", payloadMediumGzipped},
		{"hex/binary-4KiB", "hex", payloadBinaryHex},
		{"csv/small", "csv", payloadCSV},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			report(b, len(tc.in))
			for i := 0; i < b.N; i++ {
				if _, err := data.Decode(tc.in, tc.spec); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkEncode measures data.Encode, the inverse of Decode and the other
// half of every codecTransformer call.
func BenchmarkEncode(b *testing.B) {
	mustDecode := func(in []byte, spec string) data.Value {
		v, err := data.Decode(in, spec)
		if err != nil {
			b.Fatalf("setup decode %q: %v", spec, err)
		}
		return v
	}

	cases := []struct {
		name string
		spec string
		v    data.Value
	}{
		{"json/small", "json", mustDecode(payloadSmall, "json")},
		{"json/medium", "json", mustDecode(payloadMedium, "json")},
		{"json/large-200rec", "json", mustDecode(payloadLarge, "json")},
		{"json-pretty/medium", "json-pretty", mustDecode(payloadMedium, "json")},
		{"bytes/medium", "bytes", data.Bytes(payloadMedium)},
		{"base64/medium", "base64", data.Bytes(payloadMedium)},
		{"gzip/medium", "gzip", data.Bytes(payloadMedium)},
		{"csv/small", "csv", mustDecode(payloadCSV, "csv")},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			report(b, len(tc.v.Bytes()))
			for i := 0; i < b.N; i++ {
				if _, err := data.Encode(tc.v, tc.spec); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkDecodeEncodeRoundTrip isolates the full decode->encode round trip
// every codecTransformer performs per message, at the payload sizes above --
// the number every codec-aware transformer benchmark should be compared
// against to see how much of its cost is "unavoidable" codec overhead vs the
// transformer's own logic.
func BenchmarkDecodeEncodeRoundTrip(b *testing.B) {
	cases := []struct {
		name string
		in   []byte
	}{
		{"small", payloadSmall},
		{"medium", payloadMedium},
		{"large-200rec", payloadLarge},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			report(b, len(tc.in))
			for i := 0; i < b.N; i++ {
				v, err := data.Decode(tc.in, "json")
				if err != nil {
					b.Fatal(err)
				}
				if _, err := data.Encode(v, "json"); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
