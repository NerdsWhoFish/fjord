export function sourceWindow(total: number, target = 0, size = 200) {
  const start = Math.max(
    0,
    Math.min(Math.max(0, total - size), Math.floor(target / size) * size),
  );
  return { start, end: Math.min(total, start + size) };
}

export function pageRows<T>(rows: T[], page: number, size = 400) {
  const last = Math.max(0, Math.ceil(rows.length / size) - 1);
  const current = Math.max(0, Math.min(page, last));
  const start = current * size;
  return {
    rows: rows.slice(start, start + size),
    start,
    end: Math.min(rows.length, start + size),
    current,
    last,
  };
}
