import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import {
  flexRender,
  getCoreRowModel,
  getPaginationRowModel,
  getSortedRowModel,
  useReactTable,
  type ColumnDef,
  type SortingState,
} from "@tanstack/react-table";
import { ArrowDownUp, ChevronLeft, ChevronRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { type RequestRecord, outcomeLabels, timeLabel } from "@/data";

export function Status({ row }: { row: RequestRecord }) {
  return (
    <span className={`status ${row.outcome}`}>
      <span className="status-dot" />
      {row.status ? `${row.status} · ` : ""}
      {outcomeLabels[row.outcome]}
    </span>
  );
}
export function RequestTable({ rows }: { rows: RequestRecord[] }) {
  const [sorting, setSorting] = useState<SortingState>([]);
  const columns = useMemo<ColumnDef<RequestRecord>[]>(
    () => [
      {
        accessorKey: "at",
        header: "时间",
        cell: (i) => (
          <span className="mono muted">{timeLabel(i.getValue<string>())}</span>
        ),
      },
      {
        accessorKey: "id",
        header: "请求 / 客户端",
        cell: (i) => (
          <div>
            <Link
              className="request-link mono"
              to={`/requests?request=${i.row.original.id}`}
            >
              {i.getValue<string>()}
            </Link>
            <small>{i.row.original.client}</small>
          </div>
        ),
      },
      {
        accessorKey: "session",
        header: "会话",
        cell: (i) =>
          i.getValue<string>() ? (
            <Link
              className="mono"
              to={`/sessions?session=${i.getValue<string>()}`}
            >
              {i.getValue<string>()}
            </Link>
          ) : (
            <span className="muted">未识别</span>
          ),
      },
      {
        accessorKey: "egress",
        header: "实际出口",
        cell: (i) =>
          i.getValue<string>() ? (
            <Link to={`/routes?route=${i.getValue<string>()}`}>
              {i.getValue<string>()}
            </Link>
          ) : (
            <span className="muted">—</span>
          ),
      },
      {
        accessorKey: "outcome",
        header: "结果",
        cell: (i) => <Status row={i.row.original} />,
      },
      {
        accessorKey: "duration",
        header: "总耗时",
        cell: (i) => (
          <span className="mono">
            {i.getValue<number>() === undefined
              ? "—"
              : `${i.getValue<number>()} ms`}
          </span>
        ),
      },
    ],
    [],
  );
  const table = useReactTable({
    data: rows,
    columns,
    state: { sorting },
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    initialState: { pagination: { pageSize: 10 } },
  });
  return (
    <>
      <Table aria-label="请求记录">
        <TableHeader>
          {table.getHeaderGroups().map((group) => (
            <TableRow key={group.id}>
              {group.headers.map((header) => (
                <TableHead
                  key={header.id}
                  aria-sort={
                    header.column.getIsSorted() === "asc"
                      ? "ascending"
                      : header.column.getIsSorted() === "desc"
                        ? "descending"
                        : "none"
                  }
                >
                  <button
                    className="sort-button"
                    onClick={header.column.getToggleSortingHandler()}
                  >
                    {flexRender(
                      header.column.columnDef.header,
                      header.getContext(),
                    )}
                    <ArrowDownUp size={12} />
                  </button>
                </TableHead>
              ))}
            </TableRow>
          ))}
        </TableHeader>
        <TableBody>
          {table.getRowModel().rows.map((row) => (
            <TableRow key={row.original.id}>
              {row.getVisibleCells().map((cell) => (
                <TableCell key={cell.id}>
                  {flexRender(cell.column.columnDef.cell, cell.getContext())}
                </TableCell>
              ))}
            </TableRow>
          ))}
          {!rows.length && (
            <TableRow>
              <TableCell colSpan={6}>
                <div className="empty">没有匹配的请求。请调整筛选条件。</div>
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
      <div className="pagination">
        <span>
          共 {rows.length} 条 · 第 {table.getState().pagination.pageIndex + 1} /{" "}
          {Math.max(1, table.getPageCount())} 页
        </span>
        <div>
          <Button
            variant="outline"
            size="icon"
            aria-label="上一页"
            disabled={!table.getCanPreviousPage()}
            onClick={() => table.previousPage()}
          >
            <ChevronLeft />
          </Button>
          <Button
            variant="outline"
            size="icon"
            aria-label="下一页"
            disabled={!table.getCanNextPage()}
            onClick={() => table.nextPage()}
          >
            <ChevronRight />
          </Button>
        </div>
      </div>
    </>
  );
}
