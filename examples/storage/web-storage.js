const message = "This value belongs to this runtime.";
sessionStorage.setItem("message", message);
const sessionMessage = sessionStorage.getItem("message");
if (sessionMessage !== message)
	throw new Error("sessionStorage did not return the stored text");
console.log("session:", sessionMessage);

if (typeof localStorage === "undefined") {
	console.log(
		"localStorage is disabled; rerun with --local-storage <path> to persist a counter.",
	);
} else {
	const key = "example-runs";
	const previous = Number(localStorage.getItem(key) ?? "0");
	if (!Number.isSafeInteger(previous) || previous < 0) {
		throw new Error(
			`localStorage key ${key} must contain a nonnegative integer`,
		);
	}
	const current = previous + 1;
	localStorage.setItem(key, String(current));
	const saved = localStorage.getItem(key);
	if (saved !== String(current))
		throw new Error("localStorage did not return the updated counter");
	console.log("persistent runs:", saved);
}
