package option

import (
	"encoding/json"
	"testing"
)

func TestV2RayXHTTPRangeJSONCompatibility(t *testing.T) {
	for _, input := range []string{`"100-1000"`, `{"from":100,"to":1000}`} {
		var r V2RayXHTTPRange
		if err := json.Unmarshal([]byte(input), &r); err != nil {
			t.Fatalf("unmarshal %s: %v", input, err)
		}
		if r != (V2RayXHTTPRange{From: 100, To: 1000}) {
			t.Fatalf("range = %+v", r)
		}
		encoded, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if string(encoded) != `"100-1000"` {
			t.Fatalf("marshal = %s", encoded)
		}
	}
}
