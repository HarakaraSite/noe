1. Array.isArray(values)で入力を確認し、配列でなければTypeErrorを投げる。
2. typeof value === "number" && value > 0 を満たす要素を加算する。
3. reduceの初期値を0にして合計を返す。
