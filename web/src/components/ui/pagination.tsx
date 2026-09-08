import { Button } from "./button";

type PaginationProps = {
  total: number;
  page: number;
  pageSize: number;
  onPageChange: (page: number) => void;
  onPageSizeChange: (size: number) => void;
};

export function Pagination({ total, page, pageSize, onPageChange, onPageSizeChange }: PaginationProps) {
  const count = Math.max(1, Math.ceil(total / pageSize));
  const current = Math.min(Math.max(0, page), count - 1);
  const pages = Array.from(new Set([0, current - 1, current, current + 1, count - 1]))
    .filter((value) => value >= 0 && value < count).sort((a, b) => a - b);
  return (
    <nav className="pagination request-pagination" aria-label="请求分页">
      <span>共 {total} 条 · 第 {current + 1} / {count} 页</span>
      <div className="pagination-controls">
        <select aria-label="每页条数" value={pageSize} onChange={(event) => onPageSizeChange(Number(event.target.value))}>
          {[10, 20, 50, 100].map((size) => <option key={size} value={size}>{size} 条 / 页</option>)}
        </select>
        <Button variant="outline" disabled={current === 0} onClick={() => onPageChange(current - 1)}>上一页</Button>
        {pages.map((value, index) => (
          <span className="pagination-page" key={value}>
            {index > 0 && value - pages[index - 1] > 1 && <span aria-hidden="true">…</span>}
            <Button variant={value === current ? "default" : "outline"} aria-label={`第 ${value + 1} 页`} aria-current={value === current ? "page" : undefined} onClick={() => onPageChange(value)}>{value + 1}</Button>
          </span>
        ))}
        <Button variant="outline" disabled={current === count - 1} onClick={() => onPageChange(current + 1)}>下一页</Button>
      </div>
    </nav>
  );
}
