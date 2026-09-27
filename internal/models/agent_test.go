package models

import (
	"reflect"
	"testing"
)

// Providers is the one list an agent's provider is checked against.
func TestProvidersAreAnthropicOpenAIGoogleAndOther(t *testing.T) {
	if want := []string{"anthropic", "openai", "google", "other"}; !reflect.DeepEqual(Providers, want) {
		t.Fatalf("Providers = %v, want %v", Providers, want)
	}
	for _, p := range Providers {
		if !ValidProvider(p) {
			t.Errorf("ValidProvider(%q) = false, want true", p)
		}
	}
	for _, p := range []string{"", "Anthropic", "mistral"} {
		if ValidProvider(p) {
			t.Errorf("ValidProvider(%q) = true, want false", p)
		}
	}
}
