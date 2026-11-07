import { runJoseCases } from "./jose-cases.js";

const report = await runJoseCases();
console.log(
	JSON.stringify({
		sdk: report.sdk,
		version: report.version,
		algorithms: report.algorithms,
		checks: report.checks,
	}),
);
