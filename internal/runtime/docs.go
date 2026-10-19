package runtime

import (
	"noe/internal/apidocs"

	quickjs "github.com/go-quickjs/go-quickjs"
)

// installDocs registers a pure lookup function on the runtime owner thread.
// It is shared by main and Workers and never creates native I/O resources.
func installDocs(rt *quickjs.Runtime) error {
	return rt.SetModule("noe:docs", map[string]any{
		"docs": func(args ...quickjs.Value) (string, error) {
			if len(args) > 2 {
				return "", rt.ThrowTypeError("docs expects at most a topic and member")
			}
			values := [2]string{}
			for i, arg := range args {
				if arg.IsUndefined() {
					continue
				}
				if !arg.IsString() {
					return "", rt.ThrowTypeError("docs topic and member must be strings")
				}
				values[i] = arg.String()
			}
			text, err := apidocs.Lookup(values[0], values[1])
			if err != nil {
				exception := rt.NewError("Error", err.Error())
				if err := exception.Set("code", "ERR_DOC_NOT_FOUND"); err != nil {
					return "", err
				}
				return "", rt.Throw(exception)
			}
			return text, nil
		},
	})
}
