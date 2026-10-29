import { LegalPage } from "./Legal";

/** /guidelines: how to write here. */
export function Component() {
  return (
    <LegalPage title="Community guidelines">
      <p>
        Perspectiv works because strangers trust each other with what they saw. These rules keep it that way. Moderators act on
        reports, and anyone signed in can report a story, a thread post, a lore name, a place or a user.
      </p>
      <h2>Write what you were there for</h2>
      <ul>
        <li>Write about a place and a day you were actually at.</li>
        <li>Claim a character only if it was really you.</li>
      </ul>
      <h2>Protect the people in your story</h2>
      <ul>
        <li>
          Never name or tag anyone who is not a member: no real names, handles, or details that single someone out. Describe them
          as a character instead, like "the barista in the green apron".
        </li>
        <li>Members find themselves by claiming characters. Do not reveal who someone is for them.</li>
      </ul>
      <h2>Not allowed</h2>
      <ul>
        <li>Harassment, threats, or stories written to embarrass a specific person.</li>
        <li>Hateful content about people for who they are.</li>
        <li>Sexual content.</li>
        <li>Spam, advertising, or stories about places you were not at.</li>
      </ul>
      <h2>What happens</h2>
      <p>
        A moderator can hide a story, post or place, clear a lore name, or suspend an account. Hidden content is gone for
        everyone; a suspended account can still sign in and read, but cannot write.
      </p>
    </LegalPage>
  );
}
