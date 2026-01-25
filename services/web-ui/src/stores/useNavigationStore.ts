import { create } from 'zustand';
import { persist } from 'zustand/middleware';

interface NavigationState {
    selectedOrgId: number | null;
    selectedSiteId: number | null;
    selectedAreaId: number | null;

    setSelectedOrgId: (id: number | null) => void;
    setSelectedSiteId: (id: number | null) => void;
    setSelectedAreaId: (id: number | null) => void;
    clearSelection: () => void;
}

export const useNavigationStore = create<NavigationState>()(
    persist(
        (set) => ({
            selectedOrgId: null,
            selectedSiteId: null,
            selectedAreaId: null,

            setSelectedOrgId: (id) => set({
                selectedOrgId: id,
                selectedSiteId: null, // Reset child selections
                selectedAreaId: null
            }),

            setSelectedSiteId: (id) => set({
                selectedSiteId: id,
                selectedAreaId: null // Reset child selections
            }),

            setSelectedAreaId: (id) => set({
                selectedAreaId: id
            }),

            clearSelection: () => set({
                selectedOrgId: null,
                selectedSiteId: null,
                selectedAreaId: null
            }),
        }),
        {
            name: 'navigation-storage',
        }
    )
);
