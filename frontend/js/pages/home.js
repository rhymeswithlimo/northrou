// Home: featured hero, Continue Watching, and the recommendation rows.

import { $, reveal } from '../lib/dom.js';
import { heroMeta } from '../lib/format.js';
import { mountNavAutoHide } from '../components/nav.js';
import { posterCard, continueCard, row } from '../components/card.js';
import { createDetailModal } from '../components/detail.js';
import { statePanel, skeletonRow, toast, mountOfflineBanner } from '../components/states.js';
import { getHero, getContinueWatching, getHomeRows, getDetail } from '../data/library.js';
import { requireServer, requireReady, needsSetup } from '../api/connect.js';
import { isSameOrigin } from '../data/servers.js';
import { mountNativeChrome, setNativeChromeVisible } from '../components/native-chrome.js';
import { setImageSrc } from '../api/images.js';

const rowsEl = $('#rows');
const heroEl = $('#hero');

const modal = createDetailModal($('#detail-root'), {
    onSelect: openDetail,
    // A detail view is immersive. Native chrome hides while it is open and
    // comes back with it, the same way a presented view controller behaves.
    onOpen: () => setNativeChromeVisible(false),
    onClose: () => setNativeChromeVisible(true),
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

function renderHero(item) {
    const el = $('#tpl-hero').content.firstElementChild.cloneNode(true);
    const img = $('img', el);
    setImageSrc(img, item.backdrop_url);
    // The title sits in .hero__info right beside it, so the image adds nothing
    // for a screen reader.
    img.alt = '';
    $('.hero__title', el).textContent = item.title;
    $('.hero__meta', el).textContent = heroMeta(item);
    el.dataset.kind = item.kind;
    el.dataset.id = item.id;
    heroEl.replaceChildren(el);
}

async function render() {
    // Skeletons match the real grid, so the page doesn't jump when data lands.
    rowsEl.replaceChildren(skeletonRow({ count: 4, ratio: '16 / 9' }), skeletonRow());

    let hero, continuing, rows;
    try {
        [hero, continuing, rows] = await Promise.all([
            getHero(),
            getContinueWatching(),
            getHomeRows(),
        ]);
    } catch {
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

    if (hero) renderHero(hero);
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
