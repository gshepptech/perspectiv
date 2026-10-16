import { Genre } from "@/gen/perspectiv/v1/enums_pb";

/** The genre's name as readers see it; UNSPECIFIED has none. */
export function genreLabel(genre: Genre): string {
  switch (genre) {
    case Genre.DRAMA:
      return "Drama";
    case Genre.COMEDY:
      return "Comedy";
    case Genre.ROMANCE:
      return "Romance";
    case Genre.MYSTERY:
      return "Mystery";
    case Genre.HORROR:
      return "Horror";
    case Genre.SLICE_OF_LIFE:
      return "Slice of life";
    case Genre.ACTION:
      return "Action";
    case Genre.UNSPECIFIED:
      return "";
  }
}

/** Every genre an author can pick, in enum order. */
export const genres: readonly Genre[] = [
  Genre.DRAMA,
  Genre.COMEDY,
  Genre.ROMANCE,
  Genre.MYSTERY,
  Genre.HORROR,
  Genre.SLICE_OF_LIFE,
  Genre.ACTION,
];
