import { paginateRows } from './pagination.js'

function assertEqual<T>(actual: T, expected: T, label: string) {
  if (actual !== expected) {
    throw new Error(`${label}: expected ${String(expected)}, got ${String(actual)}`)
  }
}

function assertArray(actual: number[], expected: number[], label: string) {
  const matches = actual.length === expected.length && actual.every((value, index) => value === expected[index])
  if (!matches) {
    throw new Error(`${label}: expected [${expected.join(', ')}], got [${actual.join(', ')}]`)
  }
}

const rows = Array.from({ length: 100 }, (_, index) => ({ id: index + 1 }))

const first = paginateRows(rows, 1, 25)
assertEqual(first.page, 1, 'first page number')
assertEqual(first.pageCount, 4, 'first page count')
assertEqual(first.start, 1, 'first page start')
assertEqual(first.end, 25, 'first page end')
assertEqual(first.hasPrevious, false, 'first page previous')
assertEqual(first.hasNext, true, 'first page next')
assertArray(first.rows.slice(0, 5).map(row => row.id), [1, 2, 3, 4, 5], 'first page rows prefix')

const last = paginateRows(rows, 4, 25)
assertEqual(last.page, 4, 'last page number')
assertEqual(last.start, 76, 'last page start')
assertEqual(last.end, 100, 'last page end')
assertEqual(last.hasPrevious, true, 'last page previous')
assertEqual(last.hasNext, false, 'last page next')
assertArray(last.rows.slice(-3).map(row => row.id), [98, 99, 100], 'last page rows suffix')

const clamped = paginateRows(rows, 99, 25)
assertEqual(clamped.page, 4, 'page clamps to last page')
assertEqual(clamped.end, 100, 'clamped page end')

const empty = paginateRows([], 2, 25)
assertEqual(empty.page, 1, 'empty page number')
assertEqual(empty.pageCount, 1, 'empty page count')
assertEqual(empty.start, 0, 'empty page start')
assertEqual(empty.end, 0, 'empty page end')
assertEqual(empty.rows.length, 0, 'empty rows')
