package inventory

import (
	"context"
	"strings"
	"testing"
)

func TestInfrastructurePages(t *testing.T) {
	calls := 0
	rows, err := infrastructurePages(context.Background(), Request{Region: "fsn1"}, func(page int) ([]Resource, int, error) {
		calls++
		next := 0
		if page == 1 {
			next = 2
		}
		return []Resource{{Region: "global"}, {Region: "fsn1"}, {Region: "ash"}}, next, nil
	})
	if err != nil || calls != 2 || len(rows) != 4 {
		t.Fatal("pagination or regional filtering failed")
	}
	_, err = infrastructurePages(context.Background(), Request{}, func(page int) ([]Resource, int, error) { return nil, page, nil })
	if err == nil {
		t.Fatal("repeated page accepted")
	}
}

func TestDNSZoneNames(t *testing.T) {
	for _, name := range []string{"../accounts", "example.test/records", "example.test?x=1", "-invalid.test", "bad..test", ""} {
		if dnsZoneName(name) {
			t.Fatal("unsafe zone name accepted")
		}
	}
	if !dnsZoneName(strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 61)) {
		t.Fatal("valid long zone rejected")
	}
}
