// Time-aware replacement for the "Who's watching?" heading.
//
// Bucket selection is a pure function of the given Date, so it's exercisable
// without mocking the clock. The chosen phrase is then cached in localStorage
// for an hour so reloading the page (or opening it again minutes later)
// doesn't just re-roll the tagline every time - it should read as "chosen for
// now", not flicker.

const COOLDOWN_MS = 60 * 60 * 1000;
const STORAGE_KEY = 'northrou.greeting';

const pick = (arr) => arr[Math.floor(Math.random() * arr.length)];

const GENERIC = [
    "Who's watching?",
    'Ready when you are.',
    "What's the pick?",
    "Let's find something.",
];

const RULES = [
    // Weekend late night (Fri/Sat evening into the small hours).
    {
        test: (h, day) => (day === 5 || day === 6) && (h >= 22 || h < 3),
        phrases: ['Weekend binge?', 'Late one tonight?', 'Weekend movie night?'],
    },
    // Late night, any day.
    {
        test: (h) => h >= 23 || h < 3,
        phrases: ['Late night binge?', 'One more episode?', 'Burning the midnight oil?', 'One more before bed?'],
    },
    // Small hours.
    {
        test: (h) => h >= 3 && h < 5,
        phrases: ["Can't sleep?", 'Still up?', 'Night owl?', 'Insomnia watch?'],
    },
    // Early morning.
    {
        test: (h) => h >= 5 && h < 8,
        phrases: ['Early start?', 'Up with the sun?', 'Coffee first?', 'Rise and watch?'],
    },
    // Evening.
    {
        test: (h) => h >= 17 && h < 20,
        phrases: ['Evening watch?', 'Dinner and a show?', 'Evening pick?', 'Wind down?'],
    },
    // Prime time (non-weekend-special).
    {
        test: (h) => h >= 20 && h < 22,
        phrases: ['Movie night?', "Tonight's pick?", 'Prime time?', "Let's settle in."],
    },
];

function readCached(now) {
    try {
        const raw = localStorage.getItem(STORAGE_KEY);
        if (!raw) return null;
        const { phrase, expires } = JSON.parse(raw);
        if (typeof phrase !== 'string' || typeof expires !== 'number') return null;
        return now < expires ? phrase : null;
    } catch {
        return null;
    }
}

function writeCache(phrase, now) {
    try {
        localStorage.setItem(STORAGE_KEY, JSON.stringify({ phrase, expires: now + COOLDOWN_MS }));
    } catch {
        // Storage unavailable (private mode, disabled, quota) - just re-roll each call.
    }
}

/** "Who's watching?" but time-aware: 6pm -> "Evening watch?", 11pm -> "Late night binge?" */
export function watchingGreeting(date = new Date()) {
    const now = date.getTime();
    const cached = readCached(now);
    if (cached) return cached;

    const hour = date.getHours();
    const day = date.getDay();
    const rule = RULES.find((r) => r.test(hour, day));
    const phrase = pick(rule ? rule.phrases : GENERIC);
    writeCache(phrase, now);
    return phrase;
}
