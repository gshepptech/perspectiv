/**
 * Reads the mail the server sent through its SMTP mailer from Mailpit's HTTP
 * API (https://mailpit.axllent.org/docs/api-v1/). Every run signs up new
 * addresses, so searching by recipient finds only this run's mail even though
 * the dev loop's Mailpit keeps earlier runs' messages.
 */
import { stack } from "./env.ts";

type Summary = { ID: string; Subject: string; To: { Address: string }[] | null };
type SearchResult = { messages: Summary[] | null };
type Message = { ID: string; Subject: string; Text: string };

async function getJSON<T>(url: string): Promise<T> {
  const res = await fetch(url);
  if (!res.ok) throw new Error(`Mailpit ${url} answered HTTP ${res.status}`);
  return (await res.json()) as T;
}

/** The messages to `to` whose subject is `subject`, newest first. */
async function search(to: string, subject: string): Promise<Summary[]> {
  const base = stack().mailpitURL;
  const query = encodeURIComponent(`to:"${to}" subject:"${subject}"`);
  const found = await getJSON<SearchResult>(`${base}/api/v1/search?query=${query}&limit=20`);
  return (found.messages ?? []).filter(
    (m) => m.Subject === subject && (m.To ?? []).some((r) => r.Address.toLowerCase() === to.toLowerCase()),
  );
}

/**
 * Waits for the newest email to `to` with `subject` and returns its plain-text
 * body. The email is sent by a River job after the request that queued it
 * returns, so it is polled for.
 */
export async function waitForEmail(to: string, subject: string, timeoutMs = 30_000): Promise<Message> {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    const [newest] = await search(to, subject);
    if (newest) return getJSON<Message>(`${stack().mailpitURL}/api/v1/message/${encodeURIComponent(newest.ID)}`);
    if (Date.now() > deadline) throw new Error(`no email "${subject}" reached ${to} in Mailpit within ${timeoutMs}ms`);
    await new Promise((r) => setTimeout(r, 500));
  }
}

/** The first link in `text` to `path` on `origin`, with its query string. */
export function linkIn(text: string, origin: string, path: string): string | null {
  const escaped = (origin + path).replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const m = new RegExp(`${escaped}\\?[^\\s"<>]+`).exec(text);
  return m ? m[0] : null;
}

/** The verification link from the "Verify your email" message sent to `email`. */
export async function verificationLink(email: string): Promise<string> {
  const msg = await waitForEmail(email, "Verify your email");
  const link = linkIn(msg.Text, stack().baseURL, "/verify-email");
  if (!link) throw new Error(`the verification email to ${email} carries no ${stack().baseURL}/verify-email link:\n${msg.Text}`);
  return link;
}
