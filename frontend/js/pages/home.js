// Home: featured hero, Continue Watching, and the recommendation rows.

import { $, reveal } from '../lib/dom.js';
import { heroMeta } from '../lib/format.js';
import { mountNavAutoHide } from '../components/nav.js';
import { posterCard, continueCard, row } from '../components/card.js';
import { createDetailModal } from '../components/detail.js';
import { openPlayer } from '../components/player.js';
import { statePanel, skeletonRow, toast, mountOfflineBanner } from '../components/states.js';
import { getHeroPool, getHeroItem, getContinueWatching, getHomeRows, getDetail } from '../data/library.js';
import { requireServer, requireReady, needsSetup } from '../api/connect.js';
import { isSameOrigin } from '../data/servers.js';
import { mountNativeChrome, setNativeChromeVisible } from '../components/native-chrome.js';
import { resolveImageURL } from '../api/images.js';

const rowsEl = $('#rows');
const heroEl = $('#hero');

const modal = createDetailModal($('#detail-root'), {
    onSelect: openDetail,
    onPlay: (opts) => {
        if (!opts) {
            toast("That title isn't available to play yet.", { variant: 'error' });
            return;
        }
        openPlayer(opts);
    },
    // A detail view is immersive. Native chrome hides while it is open and
    // comes back with it, the same way a presented view controller behaves.
    // The hero also stops rotating under the open sheet, and resumes on close.
    onOpen: () => { stopHeroRotation(); setNativeChromeVisible(false); },
    onClose: () => { startHeroRotation(); setNativeChromeVisible(true); },
});

async function openDetail(kind, id) {
    try {
        const data = await getDetail(kind, id);
        if (data) modal.show(data);
        else toast("That title isn't in your library any more.", { variant: 'error' });
    } catch {
        toast("Couldn't load that title.", { variant: 'error' });
    }
}

// The hero rotates through the whole home-row pool at random, cross-fading to a
// fresh title on an interval. State lives at module scope so the timer and the
// modal's pause/resume hooks can reach it.
const HERO_INTERVAL_MS = 24000;
let heroPool = [];
let heroCurrent = null; // "kind:id" of what's on screen, so we don't repeat it
let heroTimer = null;
let heroRotating = false; // guards against an interval firing mid-transition

/** Build a .hero__link node for one hydrated hero item (no image yet). */
function buildHeroNode(item) {
    const el = $('#tpl-hero').content.firstElementChild.cloneNode(true);
    // The title sits in .hero__info right beside it, so the image adds nothing
    // for a screen reader.
    $('img', el).alt = '';
    $('.hero__title', el).textContent = item.title;
    $('.hero__meta', el).textContent = heroMeta(item);
    el.dataset.kind = item.kind;
    el.dataset.id = item.id;
    return el;
}

/** Resolve the backdrop blob and set it, waiting until the image is decodable
 *  so a cross-fade reveals a ready picture rather than a blank frame. */
async function loadHeroImage(node, backdropUrl) {
    const img = $('img', node);
    if (!backdropUrl) return;
    try {
        const url = await resolveImageURL(backdropUrl);
        await new Promise((res) => { img.onload = res; img.onerror = res; img.src = url; });
    } catch { /* show the title over an empty frame rather than hang */ }
}

/** Swap the hero to `node`, cross-fading over the old one when animating. */
async function showHero(node, { animate }) {
    if (!animate || !heroEl.firstElementChild) {
        heroEl.replaceChildren(node);
        return;
    }
    // Overlay the new node on the old (which stays in flow, holding the height),
    // fade it in, then drop everything else and return the new node to flow.
    node.classList.add('hero__layer', 'hero__layer--enter');
    heroEl.appendChild(node);
    void node.offsetWidth; // force a reflow so the transition actually runs
    node.classList.remove('hero__layer--enter');
    await new Promise((res) => setTimeout(res, 850));
    for (const child of [...heroEl.children]) {
        if (child !== node) child.remove();
    }
    node.classList.remove('hero__layer');
}

/** Pick a random pool title (never the current one), hydrate it, and show it.
 *  Tries a few candidates so a title with no backdrop doesn't blank the hero. */
async function pickAndShowHero({ animate }) {
    const candidates = heroPool.filter((p) => `${p.kind}:${p.id}` !== heroCurrent);
    for (let i = candidates.length - 1; i > 0; i--) { // Fisher-Yates shuffle
        const j = Math.floor(Math.random() * (i + 1));
        [candidates[i], candidates[j]] = [candidates[j], candidates[i]];
    }
    for (const p of candidates.slice(0, 5)) {
        const item = await getHeroItem(p.kind, p.id);
        if (!item) continue;
        const node = buildHeroNode(item);
        await loadHeroImage(node, item.backdrop_url);
        await showHero(node, { animate });
        heroCurrent = `${p.kind}:${p.id}`;
        return true;
    }
    return false;
}

async function rotateHero() {
    if (heroRotating || heroPool.length < 2) return;
    heroRotating = true;
    try {
        await pickAndShowHero({ animate: true });
    } finally {
        heroRotating = false;
    }
}

function startHeroRotation() {
    stopHeroRotation();
    if (heroPool.length > 1) heroTimer = setInterval(rotateHero, HERO_INTERVAL_MS);
}

function stopHeroRotation() {
    if (heroTimer) { clearInterval(heroTimer); heroTimer = null; }
}

async function render() {
    // Skeletons match the real grid, so the page doesn't jump when data lands.
    rowsEl.replaceChildren(skeletonRow({ count: 4, ratio: '16 / 9' }), skeletonRow());

    let pool, continuing, rows;
    try {
        [pool, continuing, rows] = await Promise.all([
            getHeroPool(),
            getContinueWatching(),
            getHomeRows(),
        ]);
    } catch {
        stopHeroRotation();
        heroEl.replaceChildren();
        // The hero is gone, so this panel is the only thing on the page. Centre
        // it in the viewport rather than leaving it stranded under the nav.
        const panel = statePanel({
            variant: 'error',
            title: "Couldn't reach your server",
            body: 'Northrou could not load your library. Check that the server is running, then try again.',
            action: { label: 'Try again', onClick: render },
        });
        panel.classList.add('state--fill');
        rowsEl.replaceChildren(panel);
        return;
    }

    // Seed the hero with a random pick (no fade on first paint), then let it
    // rotate. A fresh render resets the rotation so timers never stack up.
    stopHeroRotation();
    heroPool = pool ?? [];
    heroCurrent = null;
    if (await pickAndShowHero({ animate: false })) startHeroRotation();
    else heroEl.replaceChildren();

    const nodes = [];
    if (continuing.length) {
        nodes.push(row('Continue Watching', continuing, continueCard, 'row--continue'));
    }
    for (const r of rows) {
        if (r.items?.length) nodes.push(row(r.title, r.items, posterCard, undefined, r.subtitle));
    }

    // A fresh install with no media at all: say so, rather than showing a page
    // of nothing and letting it read as a failure.
    if (!nodes.length) {
        // Same slot, same treatment as the error above: the hero is empty here
        // too, so anything less would leave the page looking half-loaded.
        const panel = statePanel({
            title: 'Your library is empty',
            body: 'Add your movie and TV folders on the server with `northrou admin`, then run a scan. '
                + 'Everything you add shows up here.',
            action: { label: 'Open settings', onClick: () => window.location.assign('settings.html') },
        });
        panel.classList.add('state--fill');
        rowsEl.replaceChildren(panel);
        return;
    }

    rowsEl.replaceChildren(...nodes);
}

// One delegated listener for every card and the hero, rather than one per node.
// Scoped to `main` so cards inside the modal don't reopen it from here; the
// modal handles its own.
document.addEventListener('click', (e) => {
    const target = e.target.closest('main .card[data-id], .hero__link[data-id]');
    if (!target) return;
    e.preventDefault();
    openDetail(target.dataset.kind, target.dataset.id);
});

// A browser on a box that has not been set up yet: setup happens on the server
// itself, so all this page can do is say so, plainly, instead of pairing into
// an account that does not exist and rendering an inexplicable empty library.
function renderNeedsSetup() {
    reveal();
    const panel = statePanel({
        title: 'Almost there',
        body: 'This server has not been set up yet. In a terminal on the server, run '
            + '`northrou setup` to name it, add your media folders, and get your connection code.',
        action: { label: "I've run it", onClick: () => window.location.reload() },
    });
    panel.classList.add('state--fill');
    heroEl.replaceChildren();
    rowsEl.replaceChildren(panel);
}

// Resolve the server (same-origin or tunnel), then gate on first-run setup and
// sign-in, before building the shell. An app starts knowing nothing about which
// box it belongs to; a fresh box needs setup; a second device needs to sign in.
// Doing this first means a redirect fires ahead of any shell or skeleton flash.
const serverOk = await requireServer();
const setupPending = serverOk && isSameOrigin() && await needsSetup();
if (setupPending) {
    renderNeedsSetup();
}
if (serverOk && !setupPending && await requireReady()) {
    // Staying here: reveal before building the shell so the home skeleton is
    // what appears, not a flash on the way to a redirect that isn't happening.
    reveal();

    // On mobile the shell puts a real tab bar over the WebView and this hides
    // the web nav; everywhere else this is a no-op and the web nav stays.
    await mountNativeChrome({ current: 'home' });

    // The native sheet's close button (and its swipe-to-dismiss) drive the web
    // modal on iOS, where the web close button is hidden. Without this the two
    // halves fall out of step: the sheet goes away and the page still thinks it
    // is showing a dialog.
    window.__northrouCloseDetail = () => modal.close();

    mountNavAutoHide($('.nav'));
    mountOfflineBanner();
    render();
}
