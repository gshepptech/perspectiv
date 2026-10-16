import { Code, ConnectError } from "@connectrpc/connect";
import { QueryClient } from "@tanstack/react-query";

// Answers that a retry cannot change: the request itself is wrong or refused.
const final = new Set([
  Code.InvalidArgument,
  Code.NotFound,
  Code.AlreadyExists,
  Code.PermissionDenied,
  Code.FailedPrecondition,
  Code.Unauthenticated,
  Code.ResourceExhausted,
]);

/** Retries a failed query up to twice, but never one the server refused on its merits. */
export function shouldRetry(failureCount: number, error: unknown): boolean {
  if (final.has(ConnectError.from(error).code)) return false;
  return failureCount < 2;
}

export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: { staleTime: 30_000, retry: shouldRetry, refetchOnWindowFocus: false },
      mutations: { retry: false },
    },
  });
}
