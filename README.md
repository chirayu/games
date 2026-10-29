# Letterpress

Letterpress is a two-player word game played on a grid of letter tiles. Players
take turns building words and claiming tiles. The game ends when no neutral
tiles remain, and the player who owns the most tiles wins.

The game will typically use a 5×5 grid.

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

- A word must appear in the English dictionary. There is no minimum word
  length.
- A word cannot be played more than once.
- A word cannot be played if it is a prefix of a previously played word. For
  example, after `dogs` has been played, `dog` is not allowed.

## Winning

When no neutral tiles remain, count the tiles owned by each player. The player
with the most tiles wins. If both players own the same number of tiles, the game
ends in a tie. A completely claimed 5×5 grid has an odd number of tiles, so a
tie cannot occur on that board size.
