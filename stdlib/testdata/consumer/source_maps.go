package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func mappedExecution(ctx context.Context) error {
	code := "\"use strict\";\nfunction compiled(){\n throw new Error(\"consumer map marker\");\n}\ncompiled();\n"
	data := `{"version":3,"sources":["original.ts"],"names":["original"],"mappings":";;AAKAA"}`
	code += "//# sourceMappingURL=data:application/json;base64," + base64.StdEncoding.EncodeToString([]byte(data))
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "[consumer map]", Code: code, Virtual: true, Module: true}, stdlib.Profile{})
	if result.Code != 1 || err == nil || len(result.Diagnostics) != 1 || !strings.Contains(result.Diagnostics[0].Stack, "original.ts:6:1") || !strings.Contains(stdlib.Diagnostic(err), "consumer map marker") {
		return fmt.Errorf("mapped consumer result=%+v error=%s", result, stdlib.Diagnostic(err))
	}
	return nil
}
