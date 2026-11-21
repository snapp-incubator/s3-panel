import { Search } from 'lucide-react'
import type { FC } from 'react'

import { t } from '@/i18n'

import { Input } from '../shadcn/input'

interface SearchFieldProps {
  value: string
  onChange: (value: string) => void
  /** What is searched, for screen readers, e.g. "Search buckets". */
  label?: string
  placeholder?: string
}

const SearchField: FC<SearchFieldProps> = ({
  value,
  onChange,
  label = t('search'),
  placeholder = 'Search...'
}) => {
  return (
    <div className="relative flex w-[250px] items-center">
      <Search
        aria-hidden="true"
        className="absolute right-2 top-1/2 size-4 -translate-y-1/2"
      />
      <Input
        type="search"
        aria-label={label}
        placeholder={placeholder}
        value={value}
        onChange={e => onChange(e.target.value)}
        className="w-full pr-8"
      />
    </div>
  )
}

export default SearchField
