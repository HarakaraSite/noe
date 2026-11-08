package stdlib_test

import (
	"strings"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
)

func TestPerformanceUserTimingEntries(t *testing.T) {
	out, _ := run(t, stdlib.Config{}, `
		const nowA = performance.now();
		const nowB = performance.now();
		console.log(nowA >= 0 && nowB >= nowA, Number.isFinite(performance.timeOrigin),
		            JSON.stringify(performance.toJSON()).startsWith('{"timeOrigin":'));
		const nameDescriptor = Object.getOwnPropertyDescriptor(PerformanceEntry.prototype, "name");
		console.log(nameDescriptor.enumerable, nameDescriptor.set === undefined,
		            Performance.length, PerformanceEntry.length, PerformanceMark.length, PerformanceMeasure.length);

		const source = {nested: {value: 1}};
		const manual = new PerformanceMark("manual", {startTime: 5, detail: source});
		source.nested.value = 7;
		console.log(performance.getEntries().length, manual.name, manual.entryType,
		            manual.startTime, manual.duration, manual.detail.nested.value,
		            manual instanceof PerformanceEntry, Object.prototype.toString.call(manual),
		            Object.keys(manual).length, Reflect.set(manual, "name", "changed"),
	            manual.name, Object.prototype.hasOwnProperty.call(manual, "name"));
		manual.detail.nested.value = 9;
		console.log(manual.detail.nested.value);

		const late = performance.mark("same", {startTime: 20, detail: {value: 20}});
		performance.mark("same", {startTime: 10, detail: {value: 10}});
		performance.mark("tie-a", {startTime: 10});
		performance.mark("tie-b", {startTime: 10});
		performance.mark("end", {startTime: 25});
		const latest = performance.measure("latest", "same", "end");
		const measureDetail = {unit: "ms"};
		const reverse = performance.measure("reverse", {start: 30, end: 20, detail: measureDetail});
		measureDetail.unit = "changed";

		console.log(late === performance.getEntriesByName("same", "mark")[1],
		            latest instanceof PerformanceMeasure, latest.startTime, latest.duration,
		            reverse.startTime, reverse.duration, reverse.detail.unit);
		console.log(performance.getEntries().map(e => e.entryType + ":" + e.name + ":" + e.startTime + ":" + e.duration).join("|"));
		console.log(performance.getEntriesByName("same").map(e => e.startTime).join(","));
		console.log(performance.getEntriesByType("mark").map(e => e.name).join(","));
		console.log(JSON.stringify(latest.toJSON()));
		console.log(performance instanceof EventTarget, Object.prototype.toString.call(performance),
		            Object.keys(performance).length, typeof PerformanceObserver);

		performance.clearMarks("same");
		performance.clearMeasures("latest");
		console.log(performance.getEntries().map(e => e.entryType + ":" + e.name).join("|"));
		performance.clearMarks();
		performance.clearMeasures();
		console.log(performance.getEntries().length);
	`)
	want := strings.Join([]string{
		"true true true",
		"true true 0 0 1 0",
		"0 manual mark 5 0 1 true [object PerformanceMark] 0 false manual false",
		"9",
		"true true 10 15 30 -10 ms",
		"mark:same:10:0|mark:tie-a:10:0|mark:tie-b:10:0|measure:latest:10:15|mark:same:20:0|mark:end:25:0|measure:reverse:30:-10",
		"10,20",
		"same,tie-a,tie-b,same,end",
		`{"name":"latest","entryType":"measure","startTime":10,"duration":15,"detail":null}`,
		"true [object Performance] 0 undefined",
		"mark:tie-a|mark:tie-b|mark:end|measure:reverse",
		"0",
	}, "\n")
	if out != want {
		t.Errorf("Performance entries =\n%s\nwant\n%s", out, want)
	}
}

func TestPerformanceUserTimingValidation(t *testing.T) {
	out, _ := run(t, stdlib.Config{}, `
		const errorName = (f) => { try { f(); return "none"; } catch (e) { return e.name; } };
		console.log(errorName(() => performance.mark()),
		            errorName(() => performance.measure()),
		            errorName(() => performance.getEntriesByType()),
		            errorName(() => performance.getEntriesByName()),
		            errorName(() => new PerformanceMark()),
		            errorName(() => new Performance()),
		            errorName(() => new PerformanceEntry()),
		            errorName(() => new PerformanceMeasure()));

		const before = performance.getEntries().length;
		const failures = [
			() => performance.mark("negative", {startTime: -1}),
			() => performance.mark("nan", {startTime: NaN}),
			() => performance.mark("infinite", {startTime: Infinity}),
			() => performance.mark(Symbol("name")),
			() => performance.mark("bad-options", true),
			() => performance.measure("detail-only", {detail: 1}),
			() => performance.measure("both-forms", {start: 1, end: 2, duration: 3}),
			() => performance.measure("legacy-and-options", {start: 1}, "end"),
			() => performance.measure("missing-start", {start: "not-a-mark"}),
			() => performance.measure("negative-duration", {start: 10, duration: -2}),
		];
		console.log(failures.map(errorName).join(","));
		console.log(before === performance.getEntries().length);

		let cloneFailure;
		try { performance.mark("unclonable", {detail: () => 1}); }
		catch (e) { cloneFailure = e.name + ":" + (e instanceof DOMException); }
		console.log(cloneFailure, performance.getEntries().length);

		const valid = performance.measure("reverse", {start: 10, end: 5});
		console.log(valid.startTime, valid.duration);

		for (const value of [performance, valid]) {
			try { structuredClone(value); console.log("clone succeeded"); }
			catch (e) { console.log(e.name, e instanceof DOMException); }
		}
	`)
	want := strings.Join([]string{
		"TypeError TypeError TypeError TypeError TypeError TypeError TypeError TypeError",
		"TypeError,TypeError,TypeError,TypeError,TypeError,TypeError,TypeError,TypeError,SyntaxError,TypeError",
		"true",
		"DataCloneError:true 0",
		"10 -5",
		"DataCloneError true",
		"DataCloneError true",
	}, "\n")
	if out != want {
		t.Errorf("Performance validation =\n%s\nwant\n%s", out, want)
	}
}

func TestPerformanceWebIDLRegressions(t *testing.T) {
	out, _ := run(t, stdlib.Config{}, `
		const errorName = (f) => { try { f(); return "none"; } catch (e) { return e.name; } };
		performance.clearMarks(); performance.clearMeasures();
		performance.mark("null", {startTime: 5});
		const withNullMark = performance.measure("null-options", null);
		performance.clearMarks("null");
		let withoutNullMarkError = "none", withoutNullMark;
		try { withoutNullMark = performance.measure("null-options-empty", null); }
		catch (e) { withoutNullMarkError = e.name; }
		console.log(withNullMark.startTime, withoutNullMarkError,
		            withoutNullMark === undefined ? "missing" : withoutNullMark.startTime);

		const reads = [];
		const options = new Proxy({
			get end() { this.start = 1; return 3; },
		}, {
			get(target, key, receiver) {
				if (["detail", "duration", "end", "start"].includes(key)) reads.push(key);
				return Reflect.get(target, key, receiver);
			},
		});
		const accessorMeasure = performance.measure("accessor-order", options);
		console.log(reads.join(","), accessorMeasure.startTime, accessorMeasure.duration);

		const mark = performance.mark("brand", {startTime: 1, detail: "mark"});
		const measure = performance.measure("measure-brand", {start: 1, end: 2, detail: "measure"});
		const markDetail = Object.getOwnPropertyDescriptor(PerformanceMark.prototype, "detail").get;
		const measureDetail = Object.getOwnPropertyDescriptor(PerformanceMeasure.prototype, "detail").get;
		console.log(errorName(() => markDetail.call(measure)),
		            errorName(() => measureDetail.call(mark)),
		            errorName(() => markDetail.call({entryType: "mark", detail: "fake"})),
		            errorName(() => measureDetail.call({entryType: "measure", detail: "fake"})));
	`)
	want := strings.Join([]string{
		"0 none 0",
		"detail,duration,end,start 1 2",
		"TypeError TypeError TypeError TypeError",
	}, "\n")
	if out != want {
		t.Errorf("Performance Web IDL regressions =\n%s\nwant\n%s", out, want)
	}
}
