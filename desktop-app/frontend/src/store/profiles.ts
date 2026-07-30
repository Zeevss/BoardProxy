import { create } from "zustand";
import { persist } from "zustand/middleware";
import type { Profile } from "@/types";
import { uid } from "@/lib/utils";

interface ProfilesState {
  profiles: Profile[];
  activeId: string | null;

  createProfile: (input: ProfileInput) => Profile;
  updateProfile: (id: string, input: Partial<ProfileInput>) => void;
  deleteProfile: (id: string) => void;
  setActive: (id: string | null) => void;
  getById: (id: string | null) => Profile | undefined;
}

export type ProfileInput = Omit<Profile, "id" | "updatedAt">;

export const useProfilesStore = create<ProfilesState>()(
  persist(
    (set, get) => ({
      profiles: [],
      activeId: null,

      createProfile: (input) => {
        const profile: Profile = { ...input, id: uid("p_"), updatedAt: Date.now() };
        set((s) => ({ profiles: [...s.profiles, profile] }));
        return profile;
      },

      updateProfile: (id, input) =>
        set((s) => ({
          profiles: s.profiles.map((p) =>
            p.id === id ? { ...p, ...input, updatedAt: Date.now() } : p
          ),
        })),

      deleteProfile: (id) =>
        set((s) => {
          const profiles = s.profiles.filter((p) => p.id !== id);
          const activeId =
            s.activeId === id ? (profiles[0]?.id ?? null) : s.activeId;
          return { profiles, activeId };
        }),

      setActive: (id) => set({ activeId: id }),

      getById: (id) => get().profiles.find((p) => p.id === id),
    }),
    {
      name: "boardproxy.profiles",
      version: 2,
      // v1 хранил server/port/username/password без ключа подключения —
      // эти профили несовместимы, сбрасываем.
      migrate: () => ({ profiles: [], activeId: null }),
      partialize: (s) => ({ profiles: s.profiles, activeId: s.activeId }),
    }
  )
);
