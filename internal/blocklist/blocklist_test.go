package blocklist

import (
	"reflect"
	"testing"
)

func TestNormalizeKeepsAddressesAndNetworks(t *testing.T) {
	got, problems := Normalize([]string{" 203.0.113.7 ", "198.51.100.77/24", "2001:db8::/32", "::ffff:192.0.2.1"})
	want := []string{"203.0.113.7", "198.51.100.0/24", "2001:db8::/32", "192.0.2.1"}
	if len(problems) > 0 || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v %v", got, problems)
	}
}

func TestNormalizeRefusesWhatWouldHurt(t *testing.T) {
	_, problems := Normalize([]string{
		"0.0.0.0/0", "10.0.0.0/7", "127.0.0.1", "fe80::1", "nonsense", "2001::/8",
		"198.51.100.0/24", "198.51.100.5", "198.51.0.0/16", "198.51.100.0/24",
	})
	want := map[int]string{
		0: "too wide", 1: "too wide", 2: "not a public address", 3: "not a public address", 4: "not an address or network", 5: "too wide",
		7: "already covered by 198.51.100.0/24", 8: "covers 198.51.100.0/24 on line 7", 9: "same as line 7",
	}
	if !reflect.DeepEqual(problems, want) {
		t.Fatalf("got %v", problems)
	}
}
