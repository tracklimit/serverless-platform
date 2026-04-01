package pagination

import (
	"net/http"
	"strconv"
)

const (
	DefaultLimit = 20
	MaxLimit     = 100
)

// Params holds pagination, sorting, and filtering parameters parsed from query string.
type Params struct {
	Limit  int
	Offset int
	Sort   string
	Order  string
}

// Response wraps a paginated result set.
type Response struct {
	Items      any `json:"items"`
	TotalCount int `json:"total_count"`
	Limit      int `json:"limit"`
	Offset     int `json:"offset"`
}

// Parse extracts pagination parameters from the request query string.
func Parse(r *http.Request) Params {
	p := Params{
		Limit:  DefaultLimit,
		Offset: 0,
		Sort:   "created_at",
		Order:  "desc",
	}

	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			p.Limit = v
		}
	}
	if p.Limit > MaxLimit {
		p.Limit = MaxLimit
	}

	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			p.Offset = v
		}
	}

	if s := r.URL.Query().Get("sort"); s != "" {
		p.Sort = s
	}
	if o := r.URL.Query().Get("order"); o != "" {
		p.Order = o
	}

	return p
}
