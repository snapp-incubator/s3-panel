import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import { storageKey } from '@/lib/storageKey'

interface SidebarState {
  isOpen: boolean
  toggle: () => void
}

export const useSidebarStore = create<SidebarState>()(
  persist(
    set => ({
      isOpen: false,
      toggle: () => set(state => ({ isOpen: !state.isOpen }))
    }),
    {
      name: storageKey('sidebar-storage')
    }
  )
)
