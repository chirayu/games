# Letterpress

Letterpress is a two-player word game played on a grid of letter tiles. Players
take turns building words and claiming tiles. The game ends when no neutral
tiles remain, and the player who owns the most tiles wins.

The first implementation uses a 5×5 grid.

## Board generation

New 5×5 games use a constrained, weighted 25-letter bag. Persisted games keep
their original board unchanged.

- Choose the vowel count uniformly from 5, 6, or 7. `A E I O U` are vowels;
  `Y` is a consonant.
- Draw vowels with weights `A:82 E:127 I:70 O:75 U:28`.
- Draw other tiles with consonant weights
  `B:15 C:28 D:43 F:22 G:20 H:61 J:2 K:8 L:40 M:24 N:67 P:19 Q:1`
  `R:60 S:63 T:91 V:10 W:24 X:2 Y:20 Z:1`.
- Include no more than two difficult letters in total from `J Q X Z`, and no
  more than four copies of any individual letter.
- When a letter reaches its cap, remove it from subsequent weighted draws. When
  the difficult-letter group reaches its cap, remove all four difficult letters
  from subsequent draws.
- Shuffle the completed bag before assigning letters to row-major board
  positions.

The integer weights are product tuning constants rounded from conventional
English text letter-frequency percentages. They favor familiar, playable
boards rather than claiming to model a particular corpus. Production draws and
the shuffle use cryptographically secure randomness with unbiased bounded
sampling.

## Game rules

1. All tiles begin with a neutral colour.
2. When a player builds a word, each unprotected tile used in that word changes
   to the player's preferred colour.
3. Players may choose letters from anywhere on the grid; the tiles do not need
   to be adjacent.
4. A tile may be used only once within a word.
5. Players may use any tile to build a word, regardless of who owns it.
6. A tile becomes protected when every orthogonally adjacent tile—above, below,
   left, and right—belongs to the same player. Diagonal tiles do not count. For
   tiles on an outer edge, only adjacent tiles within the grid are considered.
   Protected tiles are shown in a darker shade.
7. The other player may use a protected tile in a word, but the tile does not
   change ownership.
8. If a protected tile is no longer surrounded by tiles belonging to the same
   player, it loses its protected status.
9. Protection is recalculated across the entire board after every turn.

## Valid words

- A selected sequence must be present in the Letterpress English dictionary,
  generated from ESDB/SCOWL size 80 with American, British, Canadian, and
  Australian spellings.
- Dictionary entries are normalized to uppercase A–Z. Words containing spaces,
  punctuation, digits, or non-ASCII letters cannot be formed from the board.
- A word cannot be played more than once.
- A word cannot be played if it is a prefix of a previously played word. For
  example, after `dogs` has been played, `dog` is not allowed.

Letters behave literally: `Q` represents `Q`, not `Qu`.

## Implementations

Different implementation approaches live in separate directories. The first is
a web client-server application under [`approaches/web-client-server`](approaches/web-client-server/).

## Winning

When no neutral tiles remain, count the tiles owned by each player. The player
with the most tiles wins. If both players own the same number of tiles, the game
ends in a tie. A completely claimed 5×5 grid has an odd number of tiles, so a
tie cannot occur on that board size.
