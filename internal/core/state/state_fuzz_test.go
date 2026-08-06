package state

import (
	"errors"
	"reflect"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
)

func FuzzStateDecode(f *testing.F) {
	valid, err := Encode(Initial("sample", "created"))
	if err != nil {
		f.Fatal(err)
	}
	for _, seed := range [][]byte{
		valid,
		{},
		[]byte(`{"schemaVersion":2}`),
		[]byte(`{"schemaVersion":1,"change":"sample","stage":"future"}`),
		append(append([]byte(nil), valid...), []byte(`{}`)...),
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, raw []byte) {
		got, err := Decode(raw, "sample")
		if err != nil {
			var refusal *failure.Refusal
			if !errors.As(err, &refusal) || refusal.Code == "" || refusal.Next == "" || !reflect.ValueOf(got).IsZero() {
				t.Fatalf("invalid state did not fail closed: state=%#v err=%v", got, err)
			}
			return
		}
		encoded, err := Encode(got)
		if err != nil {
			t.Fatal(err)
		}
		again, err := Decode(encoded, "sample")
		if err != nil || !reflect.DeepEqual(got, again) {
			t.Fatalf("state round trip drift: %#v %#v %v", got, again, err)
		}
	})
}
