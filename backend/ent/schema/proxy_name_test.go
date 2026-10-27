package schema

import (
	"strings"
	"testing"
)

func TestProxyNameUsesCharacterLimit(t *testing.T) {
	name := (Proxy{}).Fields()[0].Descriptor()
	if name.Size != 100 {
		t.Fatalf("proxy name database size changed: %d", name.Size)
	}
	for _, value := range []string{"us001", strings.Repeat("a", 100), strings.Repeat("代", 100)} {
		for _, validator := range name.Validators {
			if err := validator.(func(string) error)(value); err != nil {
				t.Errorf("valid proxy name with %d characters rejected: %v", len([]rune(value)), err)
			}
		}
	}
	for _, value := range []string{"", strings.Repeat("a", 101), strings.Repeat("代", 101)} {
		rejected := false
		for _, validator := range name.Validators {
			if err := validator.(func(string) error)(value); err != nil {
				rejected = true
			}
		}
		if !rejected {
			t.Errorf("invalid proxy name with %d characters was accepted", len([]rune(value)))
		}
	}
}
