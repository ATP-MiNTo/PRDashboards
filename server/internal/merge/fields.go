package merge

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/jira"
)

var accountID = regexp.MustCompile(`^[A-Za-z0-9:_-]{1,128}$`)

// fieldProblem says why the fields sent for a transition cannot be used.
type fieldProblem struct {
	code   string
	detail string
}

// encodeFields checks the values sent for a transition against the fields Jira asks for and shapes
// them as Jira expects: {"id": ...} for a field with allowed values, {"accountId": ...} for a user
// and the text itself for a text field. Every required field must have a value, and a field the
// transition does not have is refused.
func encodeFields(tr jira.Transition, in map[string]string) (map[string]any, *fieldProblem) {
	known := make(map[string]jira.Field, len(tr.Fields))
	for _, f := range tr.Fields {
		known[f.ID] = f
	}
	for _, id := range slices.Sorted(maps.Keys(in)) {
		if _, ok := known[id]; !ok {
			return nil, &fieldProblem{CodeInvalidField, fmt.Sprintf("%q is not a field of the transition", id)}
		}
	}
	out := make(map[string]any, len(in))
	for _, f := range tr.Fields {
		v := strings.TrimSpace(in[f.ID])
		if v == "" {
			if f.Required {
				return nil, &fieldProblem{CodeMissingField, f.Name + " is required"}
			}
			continue
		}
		switch {
		case len(f.AllowedValues) > 0:
			if !slices.ContainsFunc(f.AllowedValues, func(o jira.Option) bool { return o.ID == v }) {
				return nil, &fieldProblem{CodeInvalidField, v + " is not an option of " + f.Name}
			}
			out[f.ID] = map[string]string{"id": v}
		case f.Type == "user":
			if !accountID.MatchString(v) {
				return nil, &fieldProblem{CodeInvalidField, f.Name + " needs a Jira account id"}
			}
			out[f.ID] = map[string]string{"accountId": v}
		case f.Type == "string" || f.Type == "":
			out[f.ID] = v
		default:
			return nil, &fieldProblem{CodeInvalidField, f.Name + " has a type this app cannot fill (" + f.Type + ")"}
		}
	}
	return out, nil
}
