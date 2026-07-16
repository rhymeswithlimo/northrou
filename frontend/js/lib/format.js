// Presentation formatting. Everything here takes API field shapes and returns
// display strings; no DOM.

/** Feature length, for the detail meta line: 154 -> "2h 34m", 47 -> "47m" */
export function runtime(mins) {
    if (!mins) return '';
    const h = Math.floor(mins / 60);
    const m = mins % 60;
    return h ? `${h}h${m ? ` ${m}m` : ''}` : `${m}m`;
}

/** Episode length, which stays in whole minutes however long it runs: 62 -> "62m" */
export const minutes = (mins) => (mins ? `${mins}m` : '');

/** Seconds remaining, rounded to whole minutes: "20m left" */
export function remaining(positionSec, durationSec) {
    const left = Math.max(0, (durationSec || 0) - (positionSec || 0));
    return `${Math.round(left / 60)}m left`;
}

/** Fraction watched, as a CSS width percentage. */
export function progressPct(positionSec, durationSec) {
    if (!durationSec) return 0;
    return Math.min(100, Math.max(0, (positionSec / durationSec) * 100));
}

/** "S03:E02" */
export function episodeCode(season, number) {
    const pad = (n) => String(n).padStart(2, '0');
    return `S${pad(season)}:E${pad(number)}`;
}

/** Joins non-empty parts with the interpunct the design uses. */
export const dotted = (...parts) => parts.filter(Boolean).join(' · ');

/** Hero strapline: "2025 · Sci-Fi · 195 minutes" / "2021 · Comedy · 8 episodes" */
export function heroMeta(item) {
    const genre = item.genres?.[0];
    const tail = item.kind === 'show'
        ? (item.episode_count ? `${item.episode_count} episodes` : '')
        : (item.runtime ? `${item.runtime} minutes` : '');
    return dotted(item.year, genre, tail);
}

/** Seasons pluralised, for the detail meta line. */
export const seasonCount = (n) => `${n} Season${n === 1 ? '' : 's'}`;
