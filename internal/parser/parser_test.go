package parser

import (
	"testing"
)

func TestParseTransaction(t *testing.T) {
	tests := []struct {
		input       string
		wantType    string
		wantAmount  float64
		wantCat     string
		wantDesc    string
		expectError bool
	}{
		{
			input:      "-25000 Makan siang warteg",
			wantType:   "expense",
			wantAmount: 25000,
			wantCat:    "Makanan & Minuman",
			wantDesc:   "Makan siang warteg",
		},
		{
			input:      "+1500000 Gaji bulanan",
			wantType:   "income",
			wantAmount: 1500000,
			wantCat:    "Gaji & Upah",
			wantDesc:   "Gaji bulanan",
		},
		{
			input:      "Beli bensin 25k",
			wantType:   "expense",
			wantAmount: 25000,
			wantCat:    "Transportasi",
			wantDesc:   "bensin",
		},
		{
			input:      "Bonus 2.5jt dari project",
			wantType:   "income",
			wantAmount: 2500000,
			wantCat:    "Bonus & THR",
		},
		{
			input:       "hanya tulisan tanpa angka",
			expectError: true,
		},
	}

	for _, tt := range tests {
		res, err := ParseTransaction(tt.input)
		if tt.expectError {
			if err == nil {
				t.Errorf("input '%s' diharapkan error tapi sukses", tt.input)
			}
			continue
		}
		if err != nil {
			t.Fatalf("input '%s' error tak terduga: %v", tt.input, err)
		}
		if res.Type != tt.wantType {
			t.Errorf("input '%s' type = %s, want %s", tt.input, res.Type, tt.wantType)
		}
		if res.Amount != tt.wantAmount {
			t.Errorf("input '%s' amount = %v, want %v", tt.input, res.Amount, tt.wantAmount)
		}
		if tt.wantCat != "" && res.Category != tt.wantCat {
			t.Errorf("input '%s' category = %s, want %s", tt.input, res.Category, tt.wantCat)
		}
	}
}
