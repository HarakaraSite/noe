package stdlib_test

import (
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
)

func TestURLSearchParamsFormEncodingAndParsing(t *testing.T) {
	out, errOut := run(t, stdlib.Config{}, `
		const parsed = new URLSearchParams("a=%E0%A4A&bad=%G1%25&space=+%2B&bom=%EF%BB%BFx&unicode=%E2%9C%93")
		const weird = new URLSearchParams("??a=b")
		const tuple = new URLSearchParams([["\uD800", "x\uDC00"], ["! '()~", "*+ -._"]])
		console.log(JSON.stringify({
			pairs: [...parsed], serialized: parsed.toString(), leadingQuestion: weird.toString(),
			tuple: tuple.toString(), nullValue: new URLSearchParams(null).toString(),
			numberValue: new URLSearchParams(5).toString(),
		}).replace(/\uFEFF/g, "\\uFEFF"))
	`)
	if errOut != "" {
		t.Fatalf("stderr: %s", errOut)
	}
	want := `{"pairs":[["a","�A"],["bad","%G1%"],["space"," +"],["bom","\uFEFFx"],["unicode","✓"]],"serialized":"a=%EF%BF%BDA&bad=%25G1%25&space=+%2B&bom=%EF%BB%BFx&unicode=%E2%9C%93","leadingQuestion":"%3Fa=b","tuple":"%EF%BF%BD=x%EF%BF%BD&%21+%27%28%29%7E=*%2B+-._","nullValue":"null=","numberValue":"5="}`
	if out != want {
		t.Fatalf("got %s\nwant %s", out, want)
	}
}

func TestURLSearchParamsIDLInitializationAndArgumentConversion(t *testing.T) {
	out, errOut := run(t, stdlib.Config{}, `
		function errorName(fn) {
			try { fn(); } catch (error) { return error.constructor.name; }
			return "none";
		}
		const record = Object.create({inherited: "ignored"});
		record.first = 1;
		Object.defineProperty(record, "hidden", {value: "ignored", enumerable: false});
		Object.defineProperty(record, "\uD800", {value: "old", enumerable: true});
		record["\uFFFD"] = "new";
		const iterable = {
			*[Symbol.iterator]() { yield ["x", 1]; yield ["x", 2]; },
		};
		const copied = new URLSearchParams(iterable);
		const copyOfParams = new URLSearchParams(copied);
		copied[Symbol.iterator] = function* () { yield ["custom", "iterator"]; };
		const customCopy = new URLSearchParams(copied);
		let innerClosed = false, outerClosed = false;
		const badPair = {
			*[Symbol.iterator]() { try { yield "key"; yield Symbol("bad"); } finally { innerClosed = true; } },
		};
		const badOuter = {
			*[Symbol.iterator]() { try { yield badPair; } finally { outerClosed = true; } },
		};
		const params = new URLSearchParams("a=1&a=2");
		let conversions = 0;
		const converting = {toString() { conversions++; return "a"; }};
		const missingRequired = errorName(() => URLSearchParams.prototype.append.call(params, converting));
		const badPairError = errorName(() => new URLSearchParams([["short"]]));
		const badIterableError = errorName(() => new URLSearchParams(badOuter));
		const symbolRecordError = errorName(() => new URLSearchParams({[Symbol("key")]: "value"}));
		const symbolStringError = errorName(() => new URLSearchParams(Symbol("value")));
		params.delete("a", undefined);
		console.log(JSON.stringify({
			record: new URLSearchParams(record).toString(), iterable: copied.toString(),
			copyOfParams: copyOfParams.toString(), customCopy: customCopy.toString(),
			badPairError, badIterableError, symbolRecordError, symbolStringError,
			iteratorsClosed: innerClosed && outerClosed, missingRequired, conversions,
			hasUndefinedIsOmitted: new URLSearchParams("a=1").has("a", undefined),
			deleteUndefinedIsOmitted: params.toString(),
		}))
	`)
	if errOut != "" {
		t.Fatalf("stderr: %s", errOut)
	}
	want := `{"record":"first=1&%EF%BF%BD=new","iterable":"x=1&x=2","copyOfParams":"x=1&x=2","customCopy":"custom=iterator","badPairError":"TypeError","badIterableError":"TypeError","symbolRecordError":"TypeError","symbolStringError":"TypeError","iteratorsClosed":false,"missingRequired":"TypeError","conversions":0,"hasUndefinedIsOmitted":true,"deleteUndefinedIsOmitted":""}`
	if out != want {
		t.Fatalf("got %s\nwant %s", out, want)
	}
}

func TestURLSearchParamsBrandDescriptorsAndLiveIteration(t *testing.T) {
	out, errOut := run(t, stdlib.Config{}, `
		function errorName(fn) {
			try { fn(); } catch (error) { return error.constructor.name; }
			return "none";
		}
		const url = new URL("https://example.test/?a=1&b=2");
		const params = url.searchParams;
		const iterator = params.keys();
		const first = iterator.next().value;
		url.search = "?x=1&y=2";
		const sameObject = params === url.searchParams;
		const second = iterator.next().value;
		const values = params.values();
		const valueOne = values.next().value;
		params.toString = () => { throw Error("overridden toString was called"); };
		params.entries = () => { throw Error("overridden entries was called"); };
		params.append("z", "has space");
		const valueTwo = values.next().value;
		const valueThree = values.next().value;
		const visits = [];
		const walking = new URLSearchParams("a=1&b=2");
		walking.forEach((value, name, receiver) => {
			visits.push(name);
			if (name === "a") { receiver.delete("b"); receiver.append("c", "3"); }
		});
		const sizeGetter = Object.getOwnPropertyDescriptor(URLSearchParams.prototype, "size");
		console.log(JSON.stringify({
			first, second, sameObject, valueOne, valueTwo, valueThree,
			search: url.search, visits, tag: Object.prototype.toString.call(params),
			prototypeKeys: Object.keys(URLSearchParams.prototype),
			iteratorIdentity: URLSearchParams.prototype[Symbol.iterator] === URLSearchParams.prototype.entries,
			sizeEnumerable: sizeGetter.enumerable, sizeReadonly: sizeGetter.set === undefined,
			wrongReceiver: errorName(() => URLSearchParams.prototype.get.call({}, "x")),
			wrongReceiverBeforeConversion: (() => {
				let touched = false;
				const input = {toString() { touched = true; return "x"; }};
				const error = errorName(() => URLSearchParams.prototype.get.call({}, input));
				return error + ":" + touched;
			})(),
		}))
	`)
	if errOut != "" {
		t.Fatalf("stderr: %s", errOut)
	}
	want := `{"first":"a","second":"y","sameObject":true,"valueOne":"1","valueTwo":"2","valueThree":"has space","search":"?x=1&y=2&z=has+space","visits":["a","c"],"tag":"[object URLSearchParams]","prototypeKeys":["size","append","delete","get","getAll","has","set","sort","forEach","entries","keys","values","toString"],"iteratorIdentity":true,"sizeEnumerable":true,"sizeReadonly":true,"wrongReceiver":"TypeError","wrongReceiverBeforeConversion":"TypeError:false"}`
	if out != want {
		t.Fatalf("got %s\nwant %s", out, want)
	}
}
