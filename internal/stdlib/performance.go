package stdlib

import (
	_ "embed"

	quickjs "github.com/go-quickjs/go-quickjs"
)

//go:embed performance.js
var performanceJS string

// installPerformance replaces the minimal clock object installed by webAPIs
// with the User Timing and Performance Timeline interfaces.
func installPerformance(rt *quickjs.Runtime, host quickjs.Value) error {
	api, err := evalWithHost(rt, "<performance>", performanceJS, host)
	if err != nil {
		return err
	}
	for _, name := range []string{
		"performance", "Performance", "PerformanceEntry", "PerformanceMark", "PerformanceMeasure",
	} {
		value, err := api.Get(name)
		if err != nil {
			return err
		}
		if err := rt.Set(name, value); err != nil {
			return err
		}
	}
	return nil
}
