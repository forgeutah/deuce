// Super Threads helpers shared across the card/drawer components. Kept separate
// from the .tsx component modules so fast-refresh's "components only" rule holds.

// stripMention drops the leading @agent token from a prompt for compact display.
export function stripMention(text: string): string {
  return text.replace(/@\w+/, "").replace(/^[\s,]+/, "").trim();
}
