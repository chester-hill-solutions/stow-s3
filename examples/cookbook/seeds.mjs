import moreRecipes from "./more-recipes.mjs";

// Real recipes seeded into Stow's file history on first run.
// Each entry is a chain of revisions: the crowd refines the previous one.
// `image` is the real photograph seeded into the cookbook-images object bucket.
const builtinSeeds = [
  {
    key: "recipes/buttermilk-pancakes.md",
    image: "/seed/images/pancakes.jpg",
    revisions: [
      { file: "/seed/buttermilk-pancakes.v1.md", label: "Marta · first draft" },
      { file: "/seed/buttermilk-pancakes.md", label: "Priya · buttermilk + baking soda, rest the batter" },
    ],
  },
  {
    key: "recipes/weeknight-tomato-pasta.md",
    image: "/seed/images/pasta.jpg",
    revisions: [
      { file: "/seed/weeknight-tomato-pasta.v1.md", label: "Dave · weeknight sprint" },
      { file: "/seed/weeknight-tomato-pasta.v2.md", label: "Priya · onion, chilli, pasta water" },
      { file: "/seed/weeknight-tomato-pasta.md", label: "Marta · finish off the heat, weigh the parmesan" },
    ],
  },
  {
    key: "recipes/chocolate-chip-cookies.md",
    image: "/seed/images/cookies.jpg",
    revisions: [
      { file: "/seed/chocolate-chip-cookies.v1.md", label: "Sam · student version" },
      { file: "/seed/chocolate-chip-cookies.md", label: "Marta · chill the dough, salt, two sugars" },
    ],
  },
];

export const seeds = [...builtinSeeds, ...moreRecipes];
