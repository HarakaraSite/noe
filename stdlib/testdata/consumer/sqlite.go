package main

import (
	"context"
	"fmt"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func sqlite(ctx context.Context) error {
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "[SQLite consumer]", Virtual: true, Module: true, Code: `
import {open} from "noe:sqlite";
await using db=await open();
await db.exec("CREATE TABLE data(id INTEGER PRIMARY KEY,value TEXT,n INTEGER)");
await db.transaction(async tx=>{await tx.run("INSERT INTO data(value,n) VALUES(?,?)",["public API",9007199254740993n])});
const row=await db.get("SELECT value,n FROM data");
if(row.value!=="public API"||row.n!==9007199254740993n)throw Error("SQLite consumer result");
let denied=false;try{await open("not-allowed.db")}catch(error){denied=error.code==="ERR_FS_DISABLED"}
if(!denied)throw Error("memory SQLite enabled filesystem access");
export default "sqlite passed";`}, stdlib.Profile{})
	if err != nil || string(result.Data) != `"sqlite passed"` {
		return fmt.Errorf("SQLite consumer result=%+v error=%s", result, stdlib.Diagnostic(err))
	}
	return nil
}
