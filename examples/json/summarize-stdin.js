// printf '[100,200,300]\n' | ./noe run examples/json/summarize-stdin.js
const values = JSON.parse(process.stdin.read() ?? "");
if (!Array.isArray(values) || !values.every(value => typeof value === "number" && Number.isFinite(value))) {
  throw new TypeError("stdin must be a JSON array of finite numbers");
}

const count = values.length;
const total = values.reduce((sum, value) => sum + value, 0);
console.log(JSON.stringify({ count, total, average: count === 0 ? null : total / count }));
