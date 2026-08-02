package platformargs

import (
	"reflect"
	"testing"
)

func TestNormalizeRemovesDuplicatedAndroidExecutable(t *testing.T) {
	input := []string{"li", "/data/data/com.termux/files/usr/bin/li", "version"}
	got := Normalize(input, "android")
	want := []string{"/data/data/com.termux/files/usr/bin/li", "version"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Normalize() = %#v, want %#v", got, want)
	}
}

func TestNormalizeLeavesNormalArgumentsUntouched(t *testing.T) {
	cases := []struct {
		name string
		goos string
		args []string
	}{
		{name: "linux", goos: "linux", args: []string{"li", "version"}},
		{name: "android command", goos: "android", args: []string{"li", "version"}},
		{name: "different executable", goos: "android", args: []string{"li", "/tmp/other"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Normalize(tc.args, tc.goos)
			if !reflect.DeepEqual(got, tc.args) {
				t.Fatalf("Normalize() = %#v, want %#v", got, tc.args)
			}
		})
	}
}
