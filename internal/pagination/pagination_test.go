package pagination_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"serverless-platform/internal/pagination"
)

func TestParse_Defaults(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/functions", nil)
	p := pagination.Parse(r)

	assert.Equal(t, pagination.DefaultLimit, p.Limit)
	assert.Equal(t, 0, p.Offset)
	assert.Equal(t, "created_at", p.Sort)
	assert.Equal(t, "desc", p.Order)
}

func TestParse_CustomValues(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/functions?limit=50&offset=10&sort=name&order=asc", nil)
	p := pagination.Parse(r)

	assert.Equal(t, 50, p.Limit)
	assert.Equal(t, 10, p.Offset)
	assert.Equal(t, "name", p.Sort)
	assert.Equal(t, "asc", p.Order)
}

func TestParse_EnforcesMaxLimit(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/functions?limit=9999", nil)
	p := pagination.Parse(r)

	assert.Equal(t, pagination.MaxLimit, p.Limit)
}

func TestParse_InvalidValues(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		wantLim int
		wantOff int
	}{
		{name: "negative limit", query: "?limit=-5", wantLim: pagination.DefaultLimit, wantOff: 0},
		{name: "non-numeric limit", query: "?limit=abc", wantLim: pagination.DefaultLimit, wantOff: 0},
		{name: "negative offset", query: "?offset=-1", wantLim: pagination.DefaultLimit, wantOff: 0},
		{name: "non-numeric offset", query: "?offset=abc", wantLim: pagination.DefaultLimit, wantOff: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/functions"+tt.query, nil)
			p := pagination.Parse(r)
			assert.Equal(t, tt.wantLim, p.Limit)
			assert.Equal(t, tt.wantOff, p.Offset)
		})
	}
}
