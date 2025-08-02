package types

import (
	"testing"
)

func TestMarshal(t *testing.T) {
	for _, want := range ValidLeaseTypes() {
		var got LeaseType
		got.unmarshalString(want.String())
		if want != got {
			t.Errorf("marshal: want %q, got %q\n", want, got)
		}
	}
}

