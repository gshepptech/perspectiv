import { clients } from "@/lib/connect";

// Query keys follow ["domain", "operation", ...inputs]. The empty request is
// the caller's own profile.
export const myProfileKey = ["user", "getProfile", {}] as const;
export const preferencesKey = ["notify", "getPreferences"] as const;

export const myProfileQuery = {
  queryKey: myProfileKey,
  queryFn: () => clients.user.getProfile({}),
};

export const preferencesQuery = {
  queryKey: preferencesKey,
  queryFn: () => clients.notify.getPreferences({}),
};
