package jsonx

import (
	"math"
	"strings"
	"testing"
)

func parse(t *testing.T, text string) any {
	t.Helper()
	value, err := Parse([]byte(text))
	if err != nil {
		t.Fatalf("parsing %s: %v", text, err)
	}
	return value
}

func TestNumbersAreBinary64(t *testing.T) {
	cases := map[string]float64{
		"1":                       1,
		"1.0":                     1,
		"-0.0":                    0,
		"1e999":                   math.Inf(1),
		"-1e999":                  math.Inf(-1),
		"1e-999":                  0,
		"9007199254740993":        9007199254740992,
		"9007199254740992":        9007199254740992,
		strings.Repeat("9", 5000): math.Inf(1),
	}
	for text, want := range cases {
		got, ok := parse(t, text).(float64)
		if !ok {
			t.Fatalf("%s was not a number", text)
		}
		if got != want {
			t.Errorf("%s read as %v, expected %v", text, got, want)
		}
	}
	negative, _ := parse(t, "-0.0").(float64)
	if !math.Signbit(negative) {
		t.Errorf("-0.0 lost its sign")
	}
}

func TestALoneSurrogateIsKept(t *testing.T) {
	value, ok := parse(t, `"\ud800"`).(string)
	if !ok {
		t.Fatal("a string was not a string")
	}
	if value != "\xed\xa0\x80" {
		t.Errorf("a lone surrogate read as % x, expected its WTF-8 bytes", []byte(value))
	}
	if encoded := string(Encode(map[string]any{"v": value})); encoded != `{"v":"\ud800"}` {
		t.Errorf("a lone surrogate was written as %s", encoded)
	}
	pair, _ := parse(t, `"\ud83d\ude00"`).(string)
	if pair != "\U0001F600" {
		t.Errorf("a valid pair read as %q, expected one code point", pair)
	}
	if got := string(Encode(pair)); got != "\"\U0001F600\"" {
		t.Errorf("a valid pair was written as %s", got)
	}
}

func TestDuplicateKeysKeepTheLast(t *testing.T) {
	record, ok := parse(t, `{"a":1,"a":2}`).(map[string]any)
	if !ok {
		t.Fatal("an object was not an object")
	}
	if record["a"] != 2.0 {
		t.Errorf("a duplicate key kept %v, expected the last value", record["a"])
	}
}

func TestALeadingByteOrderMarkIsSkipped(t *testing.T) {
	value, err := Parse([]byte("\xef\xbb\xbf{\"a\":1}"))
	if err != nil {
		t.Fatalf("a body with a byte order mark failed: %v", err)
	}
	if record, ok := value.(map[string]any); !ok || record["a"] != 1.0 {
		t.Errorf("the marked body read as %v", value)
	}
}

func TestInvalidUTF8IsReplacedAsTheWhatwgDecoderDoes(t *testing.T) {
	// 0xE0 promises a second byte in A0..BF, so 0x80 fails at once and each stray 0x80 is its own error.
	body := []byte{'"', 0xE0, 0x80, 0x80, '"'}
	value, err := Parse(body)
	if err != nil {
		t.Fatalf("a body with invalid bytes failed: %v", err)
	}
	if value != "\uFFFD\uFFFD\uFFFD" {
		t.Errorf("the body read as %q, expected three replacements", value)
	}
	// A sequence cut off by the end of the body is one maximal subpart: one replacement.
	truncated := []byte{'"', 0xE2, 0x9C, '"'}
	value, err = Parse(truncated)
	if err != nil {
		t.Fatalf("a truncated sequence failed: %v", err)
	}
	if value != "\uFFFD" {
		t.Errorf("a truncated sequence read as %q, expected one replacement", value)
	}
	// A valid three-byte character is itself.
	if value, err = Parse([]byte{'"', 0xE2, 0x9C, 0x93, '"'}); err != nil || value != "\u2713" {
		t.Errorf("a valid character read as %q (%v)", value, err)
	}
}

func TestDeepNestingCases(t *testing.T) {
	const depth = 512
	body := strings.Repeat("[", depth) + strings.Repeat("]", depth)
	if _, err := Parse([]byte(body)); err != nil {
		t.Errorf("512 levels failed to parse: %v", err)
	}
}

func TestRejectsWhatIsNotJSON(t *testing.T) {
	for _, text := range []string{"", "NaN", "Infinity", "01", "1.", `{`, `[1,`, `"a`, "tru", `{"a" 1}`, "1 2", "{}x"} {
		if _, err := Parse([]byte(text)); err == nil {
			t.Errorf("%q was accepted", text)
		}
	}
}

func TestEncodeWritesANonFiniteNumberAsNull(t *testing.T) {
	got := string(Encode(map[string]any{
		"nan": math.NaN(), "inf": math.Inf(1), "ok": 4.0, "text": "a\"b\n", "list": []any{true, nil},
	}))
	want := `{"inf":null,"list":[true,null],"nan":null,"ok":4,"text":"a\"b\n"}`
	if got != want {
		t.Errorf("encoded %s, expected %s", got, want)
	}
}

func TestDecodeStreamKeepsTheMarkForItsCaller(t *testing.T) {
	text := DecodeStream([]byte("\xef\xbb\xbfdata: 1"))
	if !strings.HasPrefix(text, "\uFEFF") {
		t.Errorf("DecodeStream dropped the mark: %q", text)
	}
	if strings.HasPrefix(DecodeText([]byte("\xef\xbb\xbfdata: 1")), "\uFEFF") {
		t.Errorf("DecodeText kept the mark")
	}
}
