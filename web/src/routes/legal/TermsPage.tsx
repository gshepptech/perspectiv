import { Link } from "react-router";

import { LegalPage } from "./Legal";

/** /terms: the terms of using Perspectiv. */
export function Component() {
  return (
    <LegalPage title="Terms of use">
      <p>
        Perspectiv is a place where people who were at the same place on the same day each write what they saw. By
        creating an account you agree to these terms.
      </p>
      <h2>Who can use it</h2>
      <ul>
        <li>You must be 18 or over to have an account. Anyone can read without one.</li>
        <li>One account per person. Keep your password to yourself; you are responsible for what your account does.</li>
        <li>During the beta, signing up needs an invite.</li>
      </ul>
      <h2>What you write</h2>
      <ul>
        <li>You keep the rights to what you write. You let Perspectiv show it to readers, link it to other stories through claims, and keep it while your account exists.</li>
        <li>Write only about what you were there for. Describe other people as characters, never by real name or handle.</li>
        <li>
          Follow the <Link to="/guidelines">community guidelines</Link>. Moderators can hide anything that breaks them and suspend
          accounts that keep breaking them.
        </li>
        <li>You can edit or delete your stories, and delete your account, at any time from Settings.</li>
      </ul>
      <h2>The service</h2>
      <ul>
        <li>Perspectiv is offered as it is, in a beta. Features can change, and the service can be unavailable.</li>
        <li>We can change these terms; when we do, the date above changes, and important changes are emailed to you.</li>
      </ul>
      <p>
        How we handle your data is in the <Link to="/privacy">privacy notice</Link>.
      </p>
    </LegalPage>
  );
}
