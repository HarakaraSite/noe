export function summarizeSales(orders) {
  const products = new Map();
  let acceptedOrders = 0;
  let cancelledOrders = 0;

  for (const order of orders) {
    if (order.status === "cancelled") {
      cancelledOrders++;
      continue;
    }
    acceptedOrders++;
    const product = products.get(order.product) ?? {
      product: order.product,
      quantity: 0,
      revenue: 0,
    };
    product.quantity += order.quantity;
    product.revenue += order.quantity * order.unitPrice;
    products.set(order.product, product);
  }

  const rows = [...products.values()].sort((a, b) => b.revenue - a.revenue);
  return {
    acceptedOrders,
    cancelledOrders,
    totalQuantity: rows.reduce((total, row) => total + row.quantity, 0),
    totalRevenue: rows.reduce((total, row) => total + row.revenue, 0),
    products: rows,
  };
}

export function formatReport(title, report) {
  return [
    `# ${title}`,
    "",
    `Orders: ${report.acceptedOrders} / Cancelled: ${report.cancelledOrders}`,
    "",
    "| Product | Quantity | Revenue (JPY) |",
    "| --- | ---: | ---: |",
    ...report.products.map(row => `| ${row.product} | ${row.quantity} | ${row.revenue} |`),
    `| **Total** | **${report.totalQuantity}** | **${report.totalRevenue}** |`,
    "",
  ].join("\n");
}
