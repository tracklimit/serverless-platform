package handler

import (
	"errors"
	"fmt"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"
)

// tenantScopedMetrics are the only metric names a workspace caller may reference.
// Every metric listed here carries a `workspace` label emitted by the platform.
// Anything else (cluster-wide metrics, node_exporter, kube-state-metrics) is
// rejected so a user can't read another tenant's unscoped data.
var tenantScopedMetrics = map[string]bool{
	"platform_function_invocations_total":                  true,
	"platform_function_invocation_duration_seconds":        true,
	"platform_function_invocation_duration_seconds_bucket": true,
	"platform_function_invocation_duration_seconds_count":  true,
	"platform_function_invocation_duration_seconds_sum":    true,
}

var promqlParser = parser.NewParser(parser.Options{})

// injectWorkspace parses the PromQL expression, enforces that every vector
// selector references a tenant-scoped metric, and pins each selector's
// `workspace` label to the caller's workspace. If the caller already supplied
// a `workspace` matcher it must equal the caller's slug (MatchEqual) — any
// other form (regex, negation, different value) is rejected.
func injectWorkspace(query, workspace string) (string, error) {
	if workspace == "" {
		return "", errors.New("workspace missing from request context")
	}
	expr, err := promqlParser.ParseExpr(query)
	if err != nil {
		return "", fmt.Errorf("invalid PromQL: %w", err)
	}

	var rejectErr error
	parser.Inspect(expr, func(node parser.Node, _ []parser.Node) error {
		vs, ok := node.(*parser.VectorSelector)
		if !ok {
			return nil
		}
		if !tenantScopedMetrics[vs.Name] {
			rejectErr = fmt.Errorf("metric %q is not tenant-scoped", vs.Name)
			return rejectErr
		}
		for _, m := range vs.LabelMatchers {
			if m.Name != "workspace" {
				continue
			}
			if m.Type != labels.MatchEqual || m.Value != workspace {
				rejectErr = fmt.Errorf("workspace matcher must be workspace=%q", workspace)
				return rejectErr
			}
			return nil
		}
		vs.LabelMatchers = append(vs.LabelMatchers, &labels.Matcher{
			Type:  labels.MatchEqual,
			Name:  "workspace",
			Value: workspace,
		})
		return nil
	})
	if rejectErr != nil {
		return "", rejectErr
	}
	return expr.String(), nil
}
