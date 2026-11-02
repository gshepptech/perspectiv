import { LegalPage } from "./Legal";

/** /privacy: what Perspectiv keeps about you, and why. */
export function Component() {
  return (
    <LegalPage title="Privacy notice">
      <p>Perspectiv keeps as little about you as it can and never sells it. This is everything it keeps, and why.</p>
      <h2>What we keep</h2>
      <ul>
        <li>
          <strong>Your account:</strong> your email address, username, a one-way hash of your password (never the password
          itself), your optional bio and picture, and when you confirmed you are 18 or over. If you joined with an invite, we
          note which invite.
        </li>
        <li>
          <strong>What you do here:</strong> your stories and drafts, the characters in them, your claims, upvotes, reactions,
          thread posts, lore names, the places and writers you follow, people you block, and reports you file. When
          something you wrote is reported, a copy of it as it was then is kept with the report for moderators.
        </li>
        <li>
          <strong>Security records:</strong> your sign-in sessions, each with the kind of browser and system it started from
          (like “Firefox on macOS”, so you can tell your devices apart in Settings), and the times of sign-in attempts, so we can lock an
          account against guessing. Your network address is counted briefly to limit abuse. Each day these are deleted once
          old: a session 30 days after it ends, a sign-in attempt after 30 days, an emailed link 7 days after it is used or
          expires, and an address count after a day.
        </li>
        <li>
          <strong>Two-factor sign-in, if you turn it on:</strong> the key your authenticator app shares with us, stored
          encrypted, and your recovery codes, stored only in a form that cannot be turned back into the codes. Both are deleted
          when you turn two-factor sign-in off or delete your account. The times of codes entered are kept 30 days, like
          sign-in attempts, and a sign-in left waiting for its code is deleted a day after it expires.
        </li>
        <li>
          <strong>The waitlist:</strong> if you join it, your email address and the note you leave, until you are invited.
        </li>
      </ul>
      <h2>Who sees what</h2>
      <ul>
        <li>Your username, bio, picture and what you publish are public. Drafts are yours alone.</li>
        <li>Your email address is never shown to anyone, including other members.</li>
        <li>
          Email is sent through our email provider, which receives your address and the message. No other company receives your
          data: there are no ads, analytics or trackers.
        </li>
      </ul>
      <h2>Cookies</h2>
      <p>
        One cookie keeps you signed in. It is set only when you sign in, cannot be read by scripts, and is removed when you sign
        out. Nothing else is stored in your browser.
      </p>
      <h2>Your choices</h2>
      <ul>
        <li>Turn each kind of notification and email on or off in Settings, or have your email come as one daily summary. Every notification email also carries a link that stops them all without signing in.</li>
        <li>Edit or delete anything you wrote.</li>
        <li>Download everything we keep about your account, as one file, from Settings.</li>
        <li>
          Delete your account from Settings. Your stories are removed from Perspectiv, and your email, username, bio, picture and
          password hash are erased.
        </li>
      </ul>
    </LegalPage>
  );
}
