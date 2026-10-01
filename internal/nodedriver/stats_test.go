package nodedriver

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestSplitIPLists(t *testing.T) {
	stats := map[string]string{
		"users":                     "3",
		"user.k1.connections":       "4",
		"user.k1.active_ips":        "2",
		"user.k1.ip_list":           "203.0.113.7, ::ffff:198.51.100.2,203.0.113.7,not-an-ip,2001:db8::1",
		"user.node.ip_list":         "",
		"user.with.dots.ip_list":    "192.0.2.1",
		"user.k2.ip_list_unrelated": "x",
	}
	counters, lists := SplitIPLists(stats)
	want := map[string]string{
		"users": "3", "user.k1.connections": "4", "user.k1.active_ips": "2", "user.k2.ip_list_unrelated": "x",
	}
	if !reflect.DeepEqual(counters, want) {
		t.Fatalf("counters = %v, want %v", counters, want)
	}
	if got := lists["k1"]; !reflect.DeepEqual(got, []string{"203.0.113.7", "198.51.100.2", "2001:db8::1"}) {
		t.Fatalf("k1 = %v: duplicates and junk dropped, the mapped address unmapped", got)
	}
	if got, ok := lists["node"]; !ok || len(got) != 0 {
		t.Fatalf("an empty list is a list with nobody in it: %v %v", got, ok)
	}
	if got := lists["with.dots"]; !reflect.DeepEqual(got, []string{"192.0.2.1"}) {
		t.Fatalf("with.dots = %v", got)
	}
	if _, ok := stats["user.k1.ip_list"]; !ok {
		t.Fatal("the caller's map must be left as it was")
	}
}

func TestSplitIPListsKeepsNilAndCaps(t *testing.T) {
	if c, l := SplitIPLists(nil); c != nil || l != nil {
		t.Fatalf("nil stats: %v %v", c, l)
	}
	ips := make([]string, 0, maxUserIPs+10)
	for i := range maxUserIPs + 10 {
		ips = append(ips, "10.0."+strconv.Itoa(i/256)+"."+strconv.Itoa(i%256))
	}
	_, lists := SplitIPLists(map[string]string{"user.big.ip_list": strings.Join(ips, ",")})
	if len(lists["big"]) != maxUserIPs {
		t.Fatalf("kept %d addresses, want %d", len(lists["big"]), maxUserIPs)
	}
}
