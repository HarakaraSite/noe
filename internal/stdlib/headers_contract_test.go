package stdlib_test

import (
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
)

func TestHeadersInitWebIDLConversion(t *testing.T) {
	out, errOut := run(t, stdlib.Config{}, `
		function errorName(fn) {
			try { fn(); } catch (error) { return error.constructor.name; }
			return "none";
		}
		const prototype = {inherited: "ignored"};
		const record = Object.create(prototype);
		record.first = 1;
		Object.defineProperty(record, "hidden", {value: "ignored", enumerable: false});
		const recordHeaders = new Headers(record);
		const iterable = {
			*[Symbol.iterator]() { yield ["X-Value", 1]; yield ["X-Value", 2]; },
		};
		const iterableHeaders = new Headers(iterable);
		let convertedAfterBadPair = false;
		const lateConversion = {
			*[Symbol.iterator]() {
				yield ["short"];
				yield ["later", {toString() { convertedAfterBadPair = true; return "value"; }}];
			},
		};
		const badPairLength = errorName(() => new Headers(lateConversion));
		let closed = 0;
		const badPair = {
			[Symbol.iterator]() {
				return {
					next() { return {value: Symbol("bad"), done: false}; },
					return() { closed++; return {done: true}; },
				};
			},
		};
		const iteratorConversion = errorName(() => new Headers([badPair]));
		const symbolRecord = errorName(() => new Headers({[Symbol("name")]: "value"}));
		const symbolInit = errorName(() => new Headers(Symbol("value")));
		const nullInit = errorName(() => new Headers(null));
		const primitivePair = errorName(() => new Headers([["x", "v"], 1]));
		const iteratorReturnNotCallable = errorName(() => new Headers({
			[Symbol.iterator]: 1,
			name: "value",
		}));
		console.log(JSON.stringify({
			record: [...recordHeaders],
			recordOwnOnly: !recordHeaders.has("inherited") && !recordHeaders.has("hidden"),
			iterableValue: iterableHeaders.get("x-value"),
			badPairLength,
			convertedAfterBadPair,
			iteratorConversion,
			closed,
			symbolRecord,
			symbolInit,
			nullInit,
			primitivePair,
			iteratorReturnNotCallable,
			badEmptyPair: errorName(() => new Headers([[]])),
			badLongPair: errorName(() => new Headers([["x", "v", "extra"]])),
		}))
	`)
	if errOut != "" {
		t.Fatalf("stderr: %s", errOut)
	}
	want := `{"record":[["first","1"]],"recordOwnOnly":true,"iterableValue":"1, 2","badPairLength":"TypeError","convertedAfterBadPair":true,"iteratorConversion":"TypeError","closed":0,"symbolRecord":"TypeError","symbolInit":"TypeError","nullInit":"TypeError","primitivePair":"TypeError","iteratorReturnNotCallable":"TypeError","badEmptyPair":"TypeError","badLongPair":"TypeError"}`
	if out != want {
		t.Fatalf("got %s\nwant %s", out, want)
	}
}

func TestHeadersByteStringAndHTTPValidation(t *testing.T) {
	out, errOut := run(t, stdlib.Config{}, `
		function errorName(fn) {
			try { fn(); } catch (error) { return error.constructor.name; }
			return "none";
		}
		const headers = new Headers();
		headers.append("X-Trim", "\t\r\n value \r\n\t");
		headers.append("X-Preserve", "\u000b\f\u00a0");
		headers.append("X-Empty", "\r\n\t ");
		let valueConverted = false;
		const invalidNameAfterBothConversions = errorName(() => headers.append(" x", {
			toString() { valueConverted = true; return "value"; },
		}));
		let wrongReceiverConverted = false;
		const wrongReceiver = errorName(() => Headers.prototype.append.call({}, {
			toString() { wrongReceiverConverted = true; return "x"; },
		}, "v"));
		const symbolName = errorName(() => headers.append(Symbol("name"), "v"));
		const symbolValue = errorName(() => headers.append("x", Symbol("value")));
		const wideName = errorName(() => headers.append("x\u0100", "v"));
		const wideValue = errorName(() => headers.append("x", "v\u0100"));
		const internalCR = errorName(() => headers.append("x", "a\rb"));
		const internalLF = errorName(() => headers.append("x", "a\nb"));
		const nul = errorName(() => headers.append("x", "a\u0000b"));
		const noTrimName = errorName(() => headers.append("x ", "v"));
		const pairNameWide = errorName(() => new Headers([["x", "ok"], ["\u0100", "no"]]));
		const missingArguments = errorName(() => headers.append());
		console.log(JSON.stringify({
			trimmed: headers.get("x-trim"),
			preservedOtherWhitespace: headers.get("x-preserve").charCodeAt(0) === 11 &&
				headers.get("x-preserve").charCodeAt(1) === 12 &&
				headers.get("x-preserve").charCodeAt(2) === 160,
			empty: headers.get("x-empty"),
			invalidNameAfterBothConversions,
			valueConverted,
			wrongReceiver,
			wrongReceiverConverted,
			symbolName,
			symbolValue,
			wideName,
			wideValue,
			internalCR,
			internalLF,
			nul,
			noTrimName,
			pairNameWide,
			missingArguments,
		}))
	`)
	if errOut != "" {
		t.Fatalf("stderr: %s", errOut)
	}
	want := `{"trimmed":"value","preservedOtherWhitespace":true,"empty":"","invalidNameAfterBothConversions":"TypeError","valueConverted":true,"wrongReceiver":"TypeError","wrongReceiverConverted":false,"symbolName":"TypeError","symbolValue":"TypeError","wideName":"TypeError","wideValue":"TypeError","internalCR":"TypeError","internalLF":"TypeError","nul":"TypeError","noTrimName":"TypeError","pairNameWide":"TypeError","missingArguments":"TypeError"}`
	if out != want {
		t.Fatalf("got %s\nwant %s", out, want)
	}
}

func TestHeadersCookiesAndLiveIteration(t *testing.T) {
	out, errOut := run(t, stdlib.Config{}, `
		const headers = new Headers([
			["B", "first"], ["Set-Cookie", "a=1"], ["A", "alpha"],
			["set-cookie", "b=2"], ["b", "second"],
		]);
		const cookieCopy = headers.getSetCookie();
		cookieCopy.push("mutated");
		const ordered = [...headers];
		const live = new Headers([["b", "old"], ["a", "first"]]);
		const iterator = live.entries();
		const first = iterator.next().value;
		live.set("b", "new");
		live.append("c", "third");
		const second = iterator.next().value;
		const third = iterator.next().value;
		const done = iterator.next();
		live.append("z", "last");
		const resumed = iterator.next();
		const visited = [];
		const walking = new Headers([["a", "1"], ["b", "2"]]);
		walking.forEach((value, name, receiver) => {
			visited.push([name, value, receiver === walking]);
			if (name === "a") { receiver.delete("b"); receiver.append("c", "3"); }
		});
		console.log(JSON.stringify({
			ordered,
			getCookie: headers.get("set-cookie"),
			getCookies: headers.getSetCookie(),
			cookieCopyIsolated: headers.getSetCookie().length === 2,
			toJSONCookie: headers.toJSON()["set-cookie"],
			iteratorIdentity: Headers.prototype[Symbol.iterator] === Headers.prototype.entries,
			first,
			second,
			third,
			doneBeforeAppend: done.done,
			resumed,
			visited,
		}))
	`)
	if errOut != "" {
		t.Fatalf("stderr: %s", errOut)
	}
	want := `{"ordered":[["a","alpha"],["b","first, second"],["set-cookie","a=1"],["set-cookie","b=2"]],"getCookie":"a=1, b=2","getCookies":["a=1","b=2"],"cookieCopyIsolated":true,"toJSONCookie":"a=1, b=2","iteratorIdentity":true,"first":["a","first"],"second":["b","new"],"third":["c","third"],"doneBeforeAppend":true,"resumed":{"value":["z","last"],"done":false},"visited":[["a","1",true],["c","3",true]]}`
	if out != want {
		t.Fatalf("got %s\nwant %s", out, want)
	}
}

func TestHeadersBrandsDescriptorsAndPrivateList(t *testing.T) {
	out, errOut := run(t, stdlib.Config{}, `
		function errorName(fn) {
			try { fn(); } catch (error) { return error.constructor.name; }
			return "none";
		}
		const headers = new Headers([["x", "1"]]);
		const iterator = headers.entries();
		const append = Object.getOwnPropertyDescriptor(Headers.prototype, "append");
		const iteratorSymbol = Object.getOwnPropertyDescriptor(Headers.prototype, Symbol.iterator);
		const tag = Object.getOwnPropertyDescriptor(Headers.prototype, Symbol.toStringTag);
		const iteratorTag = Object.prototype.toString.call(iterator);
		const done = iterator.next();
		const wrongIterator = errorName(() => Headers.prototype.entries.call({}));
		const wrongKeys = errorName(() => Headers.prototype.keys.call({}));
		const wrongValues = errorName(() => Headers.prototype.values.call({}));
		const wrongNext = errorName(() => iterator.next.call({}));
		const wrongGet = errorName(() => Headers.prototype.get.call({}, Symbol("bad")));
		const noList = !("_list" in headers) && Object.keys(headers).length === 0;
		console.log(JSON.stringify({
			tag: Object.prototype.toString.call(headers),
			iteratorTag,
			noList,
			iteratorIdentity: iteratorSymbol.value === Headers.prototype.entries,
			appendEnumerable: append.enumerable,
			appendWritable: append.writable,
			appendConfigurable: append.configurable,
			appendLength: append.value.length,
			getSetCookieLength: Headers.prototype.getSetCookie.length,
			forEachLength: Headers.prototype.forEach.length,
			entriesLength: Headers.prototype.entries.length,
			tagValue: tag.value,
			tagEnumerable: tag.enumerable,
			tagWritable: tag.writable,
			methods: Object.keys(Headers.prototype),
			wrongIterator,
			wrongKeys,
			wrongValues,
			wrongNext,
			wrongGet,
			done: done.done,
		}))
	`)
	if errOut != "" {
		t.Fatalf("stderr: %s", errOut)
	}
	want := `{"tag":"[object Headers]","iteratorTag":"[object Headers Iterator]","noList":true,"iteratorIdentity":true,"appendEnumerable":true,"appendWritable":true,"appendConfigurable":true,"appendLength":2,"getSetCookieLength":0,"forEachLength":1,"entriesLength":0,"tagValue":"Headers","tagEnumerable":false,"tagWritable":false,"methods":["append","delete","get","getSetCookie","has","set","forEach","entries","keys","values"],"wrongIterator":"TypeError","wrongKeys":"TypeError","wrongValues":"TypeError","wrongNext":"TypeError","wrongGet":"TypeError","done":false}`
	if out != want {
		t.Fatalf("got %s\nwant %s", out, want)
	}
}
