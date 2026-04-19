package handler

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInjectWorkspace_AppendsMatcher(t *testing.T) {
	out, err := injectWorkspace(`sum(rate(platform_function_invocations_total{function="hello"}[1m]))`, "tenant-a")
	require.NoError(t, err)
	assert.Contains(t, out, `workspace="tenant-a"`)
	assert.Contains(t, out, `function="hello"`)
}

func TestInjectWorkspace_BareSelector(t *testing.T) {
	out, err := injectWorkspace(`platform_function_invocations_total`, "tenant-a")
	require.NoError(t, err)
	assert.Contains(t, out, `workspace="tenant-a"`)
}

func TestInjectWorkspace_HistogramQuantile(t *testing.T) {
	in := `histogram_quantile(0.99, sum by (le) (rate(platform_function_invocation_duration_seconds_bucket{function="hello"}[2m])))`
	out, err := injectWorkspace(in, "tenant-a")
	require.NoError(t, err)
	assert.Contains(t, out, `workspace="tenant-a"`)
}

func TestInjectWorkspace_AcceptsMatchingWorkspace(t *testing.T) {
	// Caller supplied workspace="tenant-a" and it matches the JWT: passthrough.
	out, err := injectWorkspace(`platform_function_invocations_total{workspace="tenant-a",function="hello"}`, "tenant-a")
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(out, `workspace=`))
}

func TestInjectWorkspace_RejectsMismatchedWorkspace(t *testing.T) {
	// Caller is tenant-a but the query tries to read tenant-b.
	_, err := injectWorkspace(`platform_function_invocations_total{workspace="tenant-b"}`, "tenant-a")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `workspace`)
}

func TestInjectWorkspace_RejectsRegexWorkspace(t *testing.T) {
	// Regex matchers bypass the "equal to my slug" check; must reject.
	_, err := injectWorkspace(`platform_function_invocations_total{workspace=~".*"}`, "tenant-a")
	require.Error(t, err)
}

func TestInjectWorkspace_RejectsNegatedWorkspace(t *testing.T) {
	_, err := injectWorkspace(`platform_function_invocations_total{workspace!="tenant-b"}`, "tenant-a")
	require.Error(t, err)
}

func TestInjectWorkspace_RejectsNonTenantMetric(t *testing.T) {
	// Cluster-wide metric — no workspace label exists to scope it.
	_, err := injectWorkspace(`sum(rate(platform_http_requests_total[1m]))`, "tenant-a")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenant-scoped")
}

func TestInjectWorkspace_RejectsNodeExporter(t *testing.T) {
	_, err := injectWorkspace(`node_cpu_seconds_total`, "tenant-a")
	require.Error(t, err)
}

func TestInjectWorkspace_RejectsEmptyWorkspace(t *testing.T) {
	_, err := injectWorkspace(`platform_function_invocations_total`, "")
	require.Error(t, err)
}

func TestInjectWorkspace_RejectsInvalidPromQL(t *testing.T) {
	_, err := injectWorkspace(`sum(rate(platform_function_invocations_total[`, "tenant-a")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid PromQL")
}

func TestInjectWorkspace_BinaryOpRewritesBothSides(t *testing.T) {
	// Both selectors must be rewritten, not just one.
	in := `sum(rate(platform_function_invocations_total{function="a"}[1m])) / sum(rate(platform_function_invocations_total{function="b"}[1m]))`
	out, err := injectWorkspace(in, "tenant-a")
	require.NoError(t, err)
	assert.Equal(t, 2, strings.Count(out, `workspace="tenant-a"`))
}
