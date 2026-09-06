import { Skeleton } from '@/components/ui/skeleton';
import { Table, TableBody, TableCell, TableRow } from '@/components/ui/table';

// The full @tanstack/react-table-backed generic DataTable (columns, sorting,
// responsive card view below 768px) lands with the nodes/keys tables in
// Task 16. This skeleton is what pages render while their first query is
// loading, so the layout doesn't jump once real rows arrive.

interface DataTableSkeletonProps {
  columns?: number;
  rows?: number;
}

export function DataTableSkeleton({ columns = 4, rows = 5 }: DataTableSkeletonProps) {
  return (
    <div className="overflow-x-auto rounded-lg border border-hairline bg-card">
      <Table>
        <TableBody>
          {Array.from({ length: rows }).map((_, r) => (
            <TableRow key={r}>
              {Array.from({ length: columns }).map((_, c) => (
                <TableCell key={c}>
                  <Skeleton className="h-3 w-full max-w-40" />
                </TableCell>
              ))}
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}
