// REPLでは1行ずつ入力。最後の行を↑で呼び出し、20を30へ編集して再実行できます。
const prices = [300, 200, 450];
let discount = 10;
function total() { return Math.round(prices.reduce((sum, price) => sum + price, 0) * (1 - discount / 100)); }
function show() { console.log(`割引 ${discount}% → 合計 ${total()}円`); }
show();
discount = 20; show();
