// ./noe run examples/sleep-demo.js
for (let step = 1; step <= 3; step++) {
  console.log(`実行 ${step}`);
  if (step < 3) await noe.sleep(1000);
}
