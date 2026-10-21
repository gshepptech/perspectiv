import { ClaimStatus } from "@/gen/perspectiv/v1/enums_pb";
import type { Character } from "@/gen/perspectiv/v1/perspective_pb";

/**
 * The character the viewer already is in a story: their PENDING or ACCEPTED
 * claim. A person is one character per story (docs/product-model.md), so
 * once there is one, nothing else in that story is open to them.
 */
export function myCharacter(characters: readonly Character[]): Character | undefined {
  return characters.find((c) => c.myClaim === ClaimStatus.PENDING || c.myClaim === ClaimStatus.ACCEPTED);
}

/** Whether the viewer could claim this character: nobody holds it and they were not turned down for it. */
export function isOpen(c: Character): boolean {
  return !c.hasAcceptedClaim && !c.hasPendingClaim && c.myClaim !== ClaimStatus.REJECTED;
}

/** Whether a story offers the viewer anything to claim. */
export function hasOpenCharacter(characters: readonly Character[]): boolean {
  return !myCharacter(characters) && characters.some(isOpen);
}
