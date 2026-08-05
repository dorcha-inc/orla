package api

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateHTTPURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{name: "http with port and path", raw: "http://localhost:9090/price"},
		{name: "https", raw: "https://example.com"},
		{name: "empty", raw: "", wantErr: "must be http or https"},
		{name: "relative path", raw: "/price", wantErr: "must be http or https"},
		{name: "host without scheme", raw: "localhost:9090/price", wantErr: "must be http or https"},
		{name: "wrong scheme", raw: "ftp://example.com", wantErr: "must be http or https"},
		{name: "scheme without host", raw: "http://", wantErr: "must include a host"},
		{name: "unparseable", raw: "http://[::1", wantErr: "is not a valid URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateHTTPURL(tt.raw)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestIsFiniteNonNegative(t *testing.T) {
	tests := []struct {
		name string
		in   float64
		want bool
	}{
		{name: "zero", in: 0, want: true},
		{name: "positive", in: 1.5, want: true},
		{name: "max float", in: math.MaxFloat64, want: true},
		{name: "negative", in: -0.1, want: false},
		{name: "nan", in: math.NaN(), want: false},
		{name: "positive infinity", in: math.Inf(1), want: false},
		{name: "negative infinity", in: math.Inf(-1), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isFiniteNonNegative(tt.in))
		})
	}
}
