package templatefuncs

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInt(t *testing.T) {
	tests := []struct {
		name    string
		value   interface{}
		want    int
		wantErr bool
	}{
		{
			name:  "int",
			value: 42,
			want:  42,
		},
		{
			name:  "json number",
			value: float64(42),
			want:  42,
		},
		{
			name:  "integer string",
			value: "42",
			want:  42,
		},
		{
			name:  "float string with integer value",
			value: "42.0",
			want:  42,
		},
		{
			name:  "json.Number",
			value: json.Number("42"),
			want:  42,
		},
		{
			name:  "negative number",
			value: float64(-42),
			want:  -42,
		},
		{
			name:    "fractional number",
			value:   float64(42.5),
			wantErr: true,
		},
		{
			name:    "fractional string",
			value:   "42.5",
			wantErr: true,
		},
		{
			name:    "non numeric string",
			value:   "not-an-int",
			wantErr: true,
		},
		{
			name:    "bool",
			value:   true,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Int(tt.value)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
