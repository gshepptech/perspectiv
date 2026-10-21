import { useQuery } from "@tanstack/react-query";
import { Bell } from "lucide-react";
import { Link } from "react-router";

import { clients } from "@/lib/connect";

/** The places a signed-in reader follows, as quick links back to them. */
export function Following() {
  const following = useQuery({
    queryKey: ["place", "listFollowing"],
    queryFn: ({ signal }) => clients.place.listFollowing({}, { signal }),
  });
  const places = following.data?.places ?? [];
  if (places.length === 0) return null;
  return (
    <nav aria-labelledby="landing-following" className="mt-16 min-w-0">
      <h2 id="landing-following" className="text-xl">
        Places you follow
      </h2>
      <ul className="mt-4 flex flex-wrap gap-2">
        {places.map((p) => (
          <li key={p.id} className="min-w-0">
            <Link
              to={`/places/${p.id}`}
              className="inline-flex min-h-11 max-w-full items-center gap-1.5 rounded-full border border-rule bg-paper px-3.5 text-sm text-ink hover:border-accent hover:text-accent sm:min-h-9"
            >
              <Bell aria-hidden size={14} className="shrink-0 text-accent" />
              <span className="min-w-0 truncate">{p.name}</span>
            </Link>
          </li>
        ))}
      </ul>
    </nav>
  );
}
