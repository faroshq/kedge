export const DEFAULT_PAGE_SIZE = 25
export const PAGE_SIZE_OPTIONS = [10, 25, 50, 100] as const

export interface PaginationState<T> {
  rows: T[]
  page: number
  pageSize: number
  pageCount: number
  total: number
  start: number
  end: number
  hasPrevious: boolean
  hasNext: boolean
}

export function normalizePageSize(pageSize: number): number {
  return Number.isFinite(pageSize) && pageSize > 0 ? Math.floor(pageSize) : DEFAULT_PAGE_SIZE
}

export function paginateRows<T>(rows: readonly T[], page: number, pageSize: number): PaginationState<T> {
  const size = normalizePageSize(pageSize)
  const total = rows.length
  const pageCount = Math.max(1, Math.ceil(total / size))
  const requestedPage = Number.isFinite(page) ? Math.floor(page) : 1
  const currentPage = Math.min(Math.max(requestedPage, 1), pageCount)
  const startIndex = total === 0 ? 0 : (currentPage - 1) * size
  const endIndex = Math.min(startIndex + size, total)

  return {
    rows: rows.slice(startIndex, endIndex),
    page: currentPage,
    pageSize: size,
    pageCount,
    total,
    start: total === 0 ? 0 : startIndex + 1,
    end: endIndex,
    hasPrevious: currentPage > 1,
    hasNext: currentPage < pageCount,
  }
}
