import {
  Pagination,
  PaginationContent,
  PaginationEllipsis,
  PaginationItem,
  PaginationLink,
  PaginationNext,
  PaginationPrevious
} from '@/components/shadcn/pagination'

const ELLIPSIS_START = 'ellipsis-start'
const ELLIPSIS_END = 'ellipsis-end'

interface CustomPaginationProps {
  totalPages: number
  currentPage: number
  onPageChange: (page: number) => void
}

const CustomPagination = ({
  totalPages,
  currentPage,
  onPageChange
}: CustomPaginationProps) => {
  const generatePageNumbers = (): (number | string)[] => {
    if (totalPages <= 5) {
      return Array.from({ length: totalPages }, (_, i) => i + 1)
    }

    const pages: (string | number)[] = [1]

    if (currentPage > 3) pages.push(ELLIPSIS_START)

    for (
      let i = Math.max(2, currentPage - 1);
      i <= Math.min(totalPages - 1, currentPage + 1);
      i++
    ) {
      pages.push(i)
    }

    if (currentPage < totalPages - 2) pages.push(ELLIPSIS_END)

    pages.push(totalPages)

    return pages
  }

  const handlePageChange = (page: number) => (e: React.MouseEvent) => {
    e.preventDefault()

    if (page !== currentPage) {
      onPageChange(page)
    }
  }

  const renderPaginationItems = () => {
    return generatePageNumbers().map(page => (
      <PaginationItem key={page}>
        {typeof page === 'number' ? (
          <PaginationLink
            href="#"
            onClick={e => {
              e.preventDefault()
              onPageChange(page)
            }}
            className={currentPage === page ? 'bg-primary text-white' : ''}
          >
            {page}
          </PaginationLink>
        ) : (
          <PaginationEllipsis />
        )}
      </PaginationItem>
    ))
  }

  return (
    <Pagination className="flex justify-center space-x-2">
      <PaginationContent>
        <PaginationItem>
          {currentPage > 1 ? (
            <PaginationPrevious
              href="#"
              onClick={handlePageChange(currentPage - 1)}
              aria-disabled={currentPage === 1}
            />
          ) : (
            <span className="mx-2 text-sm text-gray-400">Previous</span>
          )}
        </PaginationItem>

        {renderPaginationItems()}

        <PaginationItem>
          {currentPage < totalPages ? (
            <PaginationNext
              href="#"
              onClick={handlePageChange(currentPage + 1)}
              aria-disabled={currentPage === totalPages}
            />
          ) : (
            <span className="mx-2 text-sm text-gray-400">Next</span>
          )}
        </PaginationItem>
      </PaginationContent>
    </Pagination>
  )
}

export default CustomPagination
