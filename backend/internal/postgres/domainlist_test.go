package postgres

import (
	"testing"
)

// TestValidateLiterals pins the literal-interpolation guard: buildDomainList
// emits Class/StatusDim/StatusVal as raw SQL literals, so the builder must
// reject anything outside the closed sets regardless of caller validation.
func TestValidateLiterals(t *testing.T) {
	tests := []struct {
		name    string
		f       DomainListFilter
		wantErr bool
	}{
		{"empty", DomainListFilter{}, false},
		{"valid class", DomainListFilter{Class: "hero"}, false},
		{"valid flag", DomainListFilter{Flag: "broken_v6"}, false},
		{"valid status", DomainListFilter{StatusDim: "conn", StatusVal: "not_applicable"}, false},
		{"class injection", DomainListFilter{Class: "hero' OR '1'='1"}, true},
		{"flag unknown", DomainListFilter{Flag: "x') OR TRUE --"}, true},
		{"dim injection", DomainListFilter{StatusDim: "base_status = 'x' OR 1=1; --", StatusVal: "supported"}, true},
		{"status injection", DomainListFilter{StatusDim: "www", StatusVal: "supported' --"}, true},
		{"dim without value", DomainListFilter{StatusDim: "mx"}, true},
		{"value without dim", DomainListFilter{StatusVal: "supported"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.f.validateLiterals()
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateLiterals() = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
