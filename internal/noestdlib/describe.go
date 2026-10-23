package noestdlib

import (
	"context"
	"sort"
	"strings"

	"forge.harakara.site/littleisland/noe/v2/internal/apidocs"
	quickjs "github.com/go-quickjs/go-quickjs"
)

// describe separates the reference catalog from actual module exports. Linking
// discovers source-module exports without evaluating bodies or reading bindings.
func describe(ctx context.Context, rt *quickjs.Runtime, topic, member string) (map[string]any, error) {
	topics := apidocs.Topics()
	if topic == "" {
		catalog := make([]any, 0, len(topics))
		for _, item := range topics {
			catalog = append(catalog, map[string]any{"topic": item.ID, "kind": item.Kind, "summary": item.Summary})
		}
		registered, err := rt.ModuleMetadata()
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(registered))
		for _, item := range registered {
			names = append(names, item.Specifier)
		}
		return map[string]any{"catalog": catalog, "registeredModules": names,
			"note": "The catalog is documentation; registeredModules is not a complete inventory of loader-provided modules. Query a module to inspect its exports."}, nil
	}
	var known *apidocs.Topic
	for i := range topics {
		item := &topics[i]
		if item.ID == topic {
			known = item
			break
		}
		for _, alias := range item.Aliases {
			if alias == topic {
				known = item
				break
			}
		}
		if known != nil {
			break
		}
	}
	result := map[string]any{"topic": topic, "availability": "unknown", "documented": known != nil}
	if known != nil {
		result["topic"], result["kind"], result["summary"] = known.ID, known.Kind, known.Summary
		result["details"] = known.Members
	}
	if known != nil && known.Kind != "module" {
		// A documentation topic such as run_js is not an importable module.
		result["exports"] = []any{}
		return result, nil
	}
	metadata, err := rt.InspectModuleContext(ctx, topic, "")
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	exports := make([]any, 0, len(metadata.Exports))
	if err == nil {
		result["availability"] = "available"
		for _, name := range metadata.Exports {
			_, detailErr := apidocs.Lookup(topic, name)
			var signature any
			if value := apidocs.Signature(topic, name); value != "" {
				signature = value
			}
			exports = append(exports, map[string]any{"name": name, "documented": detailErr == nil, "signature": signature})
		}
	} else {
		// A loader can supply custom modules, including known catalog names.
		// Failure to inspect does not establish that such a module cannot run.
		result["inspectionError"] = "Module could not be linked for inspection; this does not prove runtime unavailability."
	}
	result["exports"] = exports
	if member != "" {
		available := "unknown"
		if err == nil {
			available = "unavailable"
			for _, name := range metadata.Exports {
				if name == member {
					available = "available"
					break
				}
			}
		}
		_, detailErr := apidocs.Lookup(topic, member)
		if detailErr == nil && strings.Contains(member, ".") && available == "unavailable" {
			available = "unknown"
		}
		var signature any
		if value := apidocs.Signature(topic, member); value != "" {
			signature = value
		}
		result["member"] = map[string]any{"name": member, "availability": available,
			"documented": detailErr == nil, "signature": signature}
	}
	if known == nil {
		suggestions := []string{}
		for _, item := range topics {
			if strings.HasPrefix(item.ID, topic) || strings.Contains(item.ID, strings.TrimPrefix(topic, "noe:")) {
				suggestions = append(suggestions, item.ID)
			}
		}
		sort.Strings(suggestions)
		result["suggestions"] = suggestions
	}
	return result, nil
}
