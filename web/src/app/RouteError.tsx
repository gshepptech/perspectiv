import { ConnectError, Code } from "@connectrpc/connect";
import { isRouteErrorResponse, Link, useRouteError } from "react-router";

function describe(error: unknown): { title: string; detail: string } {
  if (isRouteErrorResponse(error)) {
    if (error.status === 404) return { title: "Page not found", detail: "There is no page at this address." };
    return { title: `Error ${error.status}`, detail: error.statusText || "The request could not be completed." };
  }
  if (error instanceof ConnectError) {
    if (error.code === Code.NotFound) return { title: "Not found", detail: "It may have been removed or hidden." };
    if (error.code === Code.Unavailable)
      return { title: "Perspectiv is unreachable", detail: "Check your connection and try again." };
    return { title: "Something went wrong", detail: error.rawMessage };
  }
  return { title: "Something went wrong", detail: "Reload the page to try again." };
}

/** The error surface for any route: a plain page with a way home, never a blank screen. */
export function RouteError() {
  const { title, detail } = describe(useRouteError());
  return (
    <section className="mx-auto max-w-measure py-12" aria-labelledby="route-error-title">
      <h1 id="route-error-title" className="text-2xl sm:text-3xl">
        {title}
      </h1>
      <p className="mt-3 text-ink-muted">{detail}</p>
      <p className="mt-6">
        <Link to="/" className="text-accent underline underline-offset-4">
          Back to the start
        </Link>
      </p>
    </section>
  );
}

export function NotFound() {
  return (
    <section className="mx-auto max-w-measure py-12" aria-labelledby="not-found-title">
      <h1 id="not-found-title" className="text-2xl sm:text-3xl">
        Page not found
      </h1>
      <p className="mt-3 text-ink-muted">There is no page at this address.</p>
      <p className="mt-6">
        <Link to="/" className="text-accent underline underline-offset-4">
          Back to the start
        </Link>
      </p>
    </section>
  );
}
